package helpers

import (
	"fmt"
	"time"

	"github.com/ipfs/go-ipfs/Godeps/_workspace/src/golang.org/x/net/context"
	chunk "github.com/ipfs/go-ipfs/importer/chunk"
	dag "github.com/ipfs/go-ipfs/merkledag"
	"github.com/ipfs/go-ipfs/pin"
	ft "github.com/ipfs/go-ipfs/unixfs"
	u "github.com/ipfs/go-ipfs/util"
)

// BlockSizeLimit specifies the maximum size an imported block can have.
var BlockSizeLimit = 1048576 // 1 MB

// rough estimates on expected sizes
var roughDataBlockSize = chunk.DefaultBlockSize
var roughLinkBlockSize = 1 << 13 // 8KB
var roughLinkSize = 258 + 8 + 5  // sha256 multihash + size + no name + protobuf framing

// DefaultLinksPerBlock governs how the importer decides how many links there
// will be per block. This calculation is based on expected distributions of:
//  * the expected distribution of block sizes
//  * the expected distribution of link sizes
//  * desired access speed
// For now, we use:
//
//   var roughLinkBlockSize = 1 << 13 // 8KB
//   var roughLinkSize = 288          // sha256 + framing + name
//   var DefaultLinksPerBlock = (roughLinkBlockSize / roughLinkSize)
//
// See calc_test.go
var DefaultLinksPerBlock = (roughLinkBlockSize / roughLinkSize)

// ErrSizeLimitExceeded signals that a block is larger than BlockSizeLimit.
var ErrSizeLimitExceeded = fmt.Errorf("object size limit exceeded")

// UnixfsNode is a struct created to aid in the generation
// of unixfs DAG trees
type UnixfsNode struct {
	node *dag.Node
	ufmt *ft.FSNode
}

// NewUnixfsNode creates a new Unixfs node to represent a file
func NewUnixfsNode() *UnixfsNode {
	return &UnixfsNode{
		node: new(dag.Node),
		ufmt: &ft.FSNode{Type: ft.TFile},
	}
}

// NewUnixfsBlock creates a new Unixfs node to represent a raw data block
func NewUnixfsBlock() *UnixfsNode {
	return &UnixfsNode{
		node: new(dag.Node),
		ufmt: &ft.FSNode{Type: ft.TRaw},
	}
}

// NewUnixfsNodeFromDag reconstructs a Unixfs node from a given dag node
func NewUnixfsNodeFromDag(nd *dag.Node) (*UnixfsNode, error) {
	mb, err := ft.FSNodeFromBytes(nd.Data)
	if err != nil {
		return nil, err
	}

	return &UnixfsNode{
		node: nd,
		ufmt: mb,
	}, nil
}

func (n *UnixfsNode) NumChildren() int {
	return n.ufmt.NumChildren()
}

func (n *UnixfsNode) GetChild(i int, ds dag.DAGService) (*UnixfsNode, error) {
	ctx, cancel := context.WithTimeout(context.TODO(), time.Minute)
	defer cancel()

	nd, err := n.node.Links[i].GetNode(ctx, ds)
	if err != nil {
		return nil, err
	}

	return NewUnixfsNodeFromDag(nd)
}

// addChild will add the given UnixfsNode as a child of the receiver.
// the passed in DagBuilderHelper is used to store the child node an
// pin it locally so it doesnt get lost
func (n *UnixfsNode) AddChild(child *UnixfsNode, db *DagBuilderHelper) error {
	n.ufmt.AddBlockSize(child.ufmt.FileSize())

	childnode, err := child.GetDagNode()
	if err != nil {
		return err
	}

	// Add a link to this node without storing a reference to the memory
	// This way, we avoid nodes building up and consuming all of our RAM
	err = n.node.AddNodeLinkClean("", childnode)
	if err != nil {
		return err
	}

	childkey, err := db.dserv.Add(childnode)
	if err != nil {
		return err
	}

	// Pin the child node indirectly
	if db.mp != nil {
		db.mp.PinWithMode(childkey, pin.Indirect)
	}

	return nil
}

// Removes the child node at the given index
func (n *UnixfsNode) RemoveChild(index int, dbh *DagBuilderHelper) {
	k := u.Key(n.node.Links[index].Hash)
	if dbh.mp != nil {
		dbh.mp.RemovePinWithMode(k, pin.Indirect)
	}
	n.ufmt.RemoveBlockSize(index)
	n.node.Links = append(n.node.Links[:index], n.node.Links[index+1:]...)
}

func (n *UnixfsNode) SetData(data []byte) {
	n.ufmt.Data = data
}

// getDagNode fills out the proper formatting for the unixfs node
// inside of a DAG node and returns the dag node
func (n *UnixfsNode) GetDagNode() (*dag.Node, error) {
	data, err := n.ufmt.GetBytes()
	if err != nil {
		return nil, err
	}
	n.node.Data = data
	return n.node, nil
}
