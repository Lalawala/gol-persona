package gol

import "uk.ac.bris.cs/gameoflife/util"

var BrokerAliveCells = "Broker.GolAliveCells"
var Initializer = "Broker.GolInitializer"
var BrokerKey = "Broker.GolKey"
var Key = "Server.KeyGol"
var ProcessGol = "Server.ProcessWorld"
var Live = "Broker.GetLive"

//ver ProcessSegment = "worker"

// Request represents the data sent to the GOL server
type Request struct {
	World     [][]byte // The current state of the world
	Parameter Params   // Game parameters including dimensions and turns
	P         bool     // For pause
	S         bool     // For save
	K         bool
	Resume    bool
	Start     int
	End       int
}

type Response struct {
	World      [][]byte // The final state of the world
	Turns      int      // Number of completed turns
	Slice      [][]byte
	AliveCells []util.Cell // List of coordinates for alive cells
	CellCount  int
	End        bool
}
