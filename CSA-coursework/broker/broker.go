package main

import (
	"flag"
	"net"
	"net/rpc"
	"os"
	"sync"
	"uk.ac.bris.cs/gameoflife/gol"

	"uk.ac.bris.cs/gameoflife/util"
)

var mutex sync.Mutex

type Broker struct {
	Pause              bool
	Resume             chan bool
	Turn               int
	CellCount          int
	Clients            []*rpc.Client
	CombinedWorld      [][]byte
	CombinedAliveCells []util.Cell
	//LoadPredictor      *LoadPredictor
}

var addresses = []string{
	"127.0.0.1:8040",
	"127.0.0.1:8050",
	"127.0.0.1:8060",
	"127.0.0.1:8070",
}

//type LoadPredictor struct {
//	weights []float64
//}
//
////Predict and Update are both ML possible extension on halo exchange
//func (lp *LoadPredictor) Predict(features []float64) float64 {
//	// simple linear regression
//	var prediction float64
//	for i, weight := range lp.weights {
//		prediction += weight * features[i]
//	}
//	return prediction
//}
//func (lp *LoadPredictor) Update(features []float64, actualLoad float64, learningRate float64) {
//	prediction := lp.Predict(features)
//	error := actualLoad - prediction
//	for i := range lp.weights {
//		lp.weights[i] += learningRate * error * features[i]
//	}
//}

func (b *Broker) GolInitializer(req gol.Request, res *gol.Response) error {

	// Initialize the game world and distribute tasks to GOL workers
	world := copySlice(req.World)
	b.Resume = make(chan bool)
	b.CombinedWorld = make([][]byte, req.Parameter.ImageHeight)
	for i := range b.CombinedWorld {
		b.CombinedWorld[i] = make([]byte, req.Parameter.ImageWidth)
	}
	// Synchronization
	if req.Parameter.Turns == 0 {
		if b.Pause {
			<-b.Resume
		}
		b.CombinedWorld = copySlice(req.World)

	} else {
		//define a slicePool to reuse [][]byte slices
		var slicePool = sync.Pool{
			New: func() interface{} {
				return make([][]byte, 0)
			},
		}

		for i := 0; i < req.Parameter.Turns; i++ {
			mutex.Lock()
			if b.Pause {
				mutex.Unlock()
				<-b.Resume
			} else {
				mutex.Unlock()
			}

			var wg sync.WaitGroup
			nodes := len(b.Clients)
			responses := make([][][]byte, len(b.Clients))
			for i, client := range b.Clients {
				wg.Add(1)
				go func(i int, client *rpc.Client) {
					defer wg.Done()
					haloSize := req.Parameter.ImageHeight / nodes
					start := i * haloSize
					end := (i + 1) * haloSize
					if i == nodes-1 {
						end = req.Parameter.ImageHeight
					}
					//newReq := req
					segment := slicePool.Get().([][]byte)
					//create a segment with haloSize+2 rows
					segment = segment[:haloSize+2]
					for k := range segment {
						segment[k] = make([]byte, req.Parameter.ImageWidth)
					}
					//copy the world to the segment
					for j := 0; j < haloSize; j++ {
						copy(segment[j+1], world[start+j])
					}

					if i == 0 {
						copy(segment[0], world[req.Parameter.ImageHeight-1])
					} else {
						copy(segment[0], world[start-1])
					}
					//copy the halo rows to the segment
					if i == nodes-1 {
						copy(segment[haloSize+1], world[0])
					} else {
						copy(segment[haloSize+1], world[end])
					}
					// //process the segment
					// processedSegment := processSegment(segment, req.Parameter)

					// for j := 0; j < haloSize; j++ {
					// 	mutex.Lock()
					// 	world[start+j] = processedSegment[j]
					// 	mutex.Unlock()
					// }

					processedSegment := processSegment(segment, req.Parameter)
					responses[i] = processedSegment
				}(i, client)

			}
			wg.Wait()
			// Combine results
			for i, response := range responses {
				start := i * (req.Parameter.ImageHeight / nodes)
				for j := range response {
					copy(world[start+j], response[j])
				}
			}
		}

	}

	// Iterate over each client to perform halo row exchange and processing
	//for i, client := range b.Clients {
	//	newReq := req
	//	haloSize := newReq.Parameter.ImageHeight / len(b.Clients)
	//	newReq.Start = i * haloSize
	//	newReq.End = (i + 1) * haloSize
	//	// Ensure the last client processes up to the image height
	//	if i == len(b.Clients)-1 {
	//		newReq.End = newReq.Parameter.ImageHeight
	//	}
	//	//
	//}

	// Finalize
	res.World = copySlice(b.CombinedWorld)
	res.AliveCells = calculateAliveCells(req.Parameter, b.CombinedWorld)
	res.End = true
	return nil

	return nil
}

func processSegment(segment [][]byte, params gol.Params) [][]byte {
	processed := make([][]byte, len(segment)-2)
	for i := range processed {
		processed[i] = make([]byte, len(segment[i+1]))
		for j := range processed[i] {
			// Implement efficient Game of Life logic here
			// Example: only update if the state changes
			processed[i][j] = segment[i+1][j] // Placeholder logic
		}
	}
	return processed
}

func (b *Broker) GolAliveCells(req gol.Request, res *gol.Response) error {
	mutex.Lock()
	defer mutex.Unlock()
	return nil
}

func (b *Broker) GolKey(req gol.Request, res *gol.Response) error {
	var wg sync.WaitGroup
	//broker := &Broker{}
	if req.S {
		mutex.Lock()
		res.Turns = b.Turn
		res.World = copySlice(b.CombinedWorld)
		mutex.Unlock()
	} else if req.P {
		mutex.Lock()
		b.Pause = !b.Pause
		mutex.Unlock()
		if !b.Pause {
			b.Resume <- true
		}
	} else if req.K {
		for _, client := range b.Clients {
			wg.Add(1)
			go func(client *rpc.Client) {
				defer wg.Done()
				client.Call(gol.Key, req, res)
			}(client)
		}
		wg.Wait()
		os.Exit(0)
	}
	return nil

}

func calculateAliveCells(p gol.Params, world [][]byte) []util.Cell {
	var cells []util.Cell
	for row := 0; row < p.ImageHeight; row++ {
		for col := 0; col < p.ImageWidth; col++ {
			if world[row][col] == 255 {
				cells = append(cells, util.Cell{X: col, Y: row})
			}
		}
	}
	return cells
}

func copySlice(src [][]byte) [][]byte {
	dst := make([][]byte, len(src))
	for i := range src {
		dst[i] = make([]byte, len(src[i]))
		copy(dst[i], src[i])
	}
	return dst
}

/*TODO:
1.Client Connections:
Establishes RPC connections to multiple GOL workers using their respective addresses.
These connections allow the Broker to distribute tasks and collect results from the workers.

2.Broker Initialization:
Creates a Broker instance, initializing its fields such as Clients, Resume, Turn, and CellCount.
The Resume channel is used to manage pause and resume functionality.

3. RPC Registration:
Registers the Broker instance as an RPC service, enabling it to handle incoming RPC calls from the Local Controller.

4.Listener Setup:
Sets up a TCP listener on a specified port (e.g., 8030) to accept incoming connections from the Local Controller.
This allows the Local Controller to communicate with the Broker via RPC.

5.RPC Acceptance:
Continuously accepts and serves incoming RPC requests from the Local Controller.
Overall, the main function sets up the necessary infrastructure for the Broker to function as an intermediary between the Local Controller and the GOL workers.
*/
func main() {
	clients := make([]*rpc.Client, 4)
	// Broker Initialization
	broker := &Broker{
		Resume:    make(chan bool),
		Turn:      0,
		CellCount: 0,
		Clients:   clients,
		//LoadPredictor: &LoadPredictor{
		//	weights: []float64{rand.Float64(), rand.Float64()}, // 随机初始化权重
		//},
	}

	// Register the broker
	if err := rpc.Register(broker); err != nil {
		return
	}

	// Parse the port flag
	port := flag.String("port", "8030", "port to listen on")
	flag.Parse()

	// Create a listener
	listener, err := net.Listen("tcp", ":"+*port)
	if err != nil {
		panic(err)
	}
	defer listener.Close()

	// Accept RPC connections
	rpc.Accept(listener)
}

//// Send halo rows to neighboring workers
//// If the current client is not the first, send the first row of the current segment to the previous client
//if i > 0 {
//	previousClient := b.Clients[i-1]
//	previousClient.Call("Worker.ReceiveHalo", world[i*haloSize-1], nil)
//}
//// If the current client is not the last, send the last row of the current segment to the next client
//if i < len(b.Clients)-1 {
//	nextClient := b.Clients[i+1]
//	nextClient.Call("Worker.ReceiveHalo", world[(i+1)*haloSize], nil)
//}
//
//// Create a new response object
//newRes := new(gol.Response)
//// Call the ProcessGol method on the current client to process its assigned world segment
//client.Call("Worker.ProcessGol", newReq, newRes)
//// Collect results
//// ... existing code to collect and merge results ...
