package pastry

import (
	"fmt"
)

// TimeoutError represents an error that was raised when a call has taken too long. It is its own type for the purposes of handling the error.
type TimeoutError struct {
	Action  string
	Timeout int
}

// Error returns the TimeoutError as a string and fulfills the error interface.
func (t TimeoutError) Error() string {
	return fmt.Sprintf("Timeout error: %s took more than %d seconds.", t.Action, t.Timeout)
}

// throwTimeout creates a new TimeoutError from the action and timeout specified.
func throwTimeout(action string, timeout int) TimeoutError {
	return TimeoutError{
		Action:  action,
		Timeout: timeout,
	}
}

// routingTableRequest is a request for a specific Node in the routing table. It is simply the row, column, and entry that is to be retrieved, along with the channel that the Node is to be passed to when it has been retrieved.
type routingTableRequest struct {
	row   int
	col   int
	entry int
	resp  chan Node
}

// Node represents a specific server in the cluster.
type Node struct {
	LocalIP   string // The IP through which the Node should be accessed by other Nodes with an identical Region
	GlobalIP  string // The IP through which the Node should be accessed by other Nodes whose Region differs
	Port      int    // The port the Node is listening on
	Region    string // A string that allows you to intelligently route between local and global requests for, e.g., EC2 regions
	ID        NodeID
	proximity int64 // The raw proximity score for the Node, not adjusted for Region
}

// RoutingTable is what a Node uses to route requests through the cluster.
// RoutingTables have 32 rows of 16 columns each, and each column has an indeterminate number of entries in it.
// A Node's row in the RoutingTable is the index of the first significant digit between the Node and the Node the RoutingTable belongs to.
// A Node's column in the RoutingTable is the numerical value of the first significant digit between the Node and the Node the RoutingTable belongs to.
// A Node's position in the column is determined by ordering all Nodes in that column by proximity to the Node the RoutingTable belongs to.
//
// RoutingTables are concurrency-safe; the only way to interact with the RoutingTable is through channels.
type RoutingTable struct {
	self  Node
	nodes [32][16][]Node
	input chan Node
	req   chan routingTableRequest
	kill  chan bool
}

// Insert inserts a new Node into the RoutingTable.
func (t *RoutingTable) Insert(n Node) {
	t.input <- n
}

// GetNode retrieves a Node from the RoutingTable based on its row, column, and position. The Node is returned, or an error. Note that a nil response from both variables signifies invalid query parameters; either the row, column, or entry was outside the bounds of the table.
//
// GetNode is concurrency-safe, and will return a TimeoutError if it is blocked for more than one second.
func (t *RoutingTable) GetNode(row, col, entry int) (n Node, err error) {
	select {
	case n = <-getNode(row, col, entry):
		return n, nil
	case time.After(1 * time.Second):
		return nil, throwTimeout("Node retrieval", 1)
	}
}

// getNode is the low-level implementation of Node retrieval. It takes care of the actual retrieval of Nodes, creation of the routingTableRequest, and returns the response channel.
func (t *RoutingTable) getNode(row, col, entry int) chan Node {
	resp := make(chan Node)
	t.req <- routingTableRequest{row: row, col: col, entry: entry, resp: resp}
	return resp
}

// listen is a low-level helper that will set the RoutingTable listening for requests and inserts. Passing a value to the RoutingTable's kill property will break the listen loop.
func (t *RoutingTable) listen() {
	for {
		select {
		case n := <-t.input:
			//TODO: Insert n into the table
			break
		case r := <-t.req:
			if r.row > 32 {
				r.resp <- nil
				break
			}
			if r.col > 16 {
				r.resp <- nil
				break
			}
			if r.entry > len(t.nodes[row][col]) {
				r.resp <- nil
				break
			}
			r.resp <- t.nodes[row][col][entry]
			break
		case k := <-t.kill:
			return
		}
	}
}

// Neighborhood contains the 32 closest Nodes to the current Node, based on the amount of time a request takes to complete (with a multiplier for Nodes in a different Region, in an attempt to keep requests within a Region).
//
// The Neighborhood is not used in routing, it is only maintained for ordering entries within columns of the RoutingTable.
type Neighborhood [32]Node

// LeafSet contains the 32 closest Nodes to the current Node, based on the numerical proximity of their NodeIDs.
//
// The LeafSet is divided into Left and Right; the NodeID space is considered to be circular and thus wraps around. Left contains NodeIDs less than the current NodeID. Right contains NodeIDs greater than the current NodeID.
type LeafSet struct {
	Left  [16]Node
	Right [16]Node
}