package distributed_fixture

import "net/rpc"

// CallEcho exercises the net/rpc client.Call detector.
// Expected:
//
//	NodeMessageType qname="rpc:EchoService.Echo"
//	rpc_calls(CallEcho -> rpc:EchoService.Echo)
func CallEcho(c *rpc.Client) error {
	args := EchoArgs{Message: "hi"}
	reply := EchoReply{}
	return c.Call("EchoService.Echo", args, &reply)
}

// CallSomethingElse exercises the form with a second RPC method.
// Expected: rpc:Math.Add MessageType + rpc_calls edge.
func CallSomethingElse(c *rpc.Client) error {
	var reply int
	return c.Call("Math.Add", []int{1, 2}, &reply)
}

// CallDynamicTarget passes a runtime-built target string. Expected:
// NO rpc_calls edge (V0 only handles string literals).
func CallDynamicTarget(c *rpc.Client, method string) error {
	var reply int
	return c.Call(method, nil, &reply)
}

// CallNonRPC is a method named Call on a non-rpc type with a string-literal
// first arg. We accept this currently emits an edge — distinguishing it
// from a real net/rpc.Client.Call requires receiver-type filtering, deferred.
type FakeClient struct{}

func (f *FakeClient) Call(method string, args, reply interface{}) error { return nil }

// FakeUse intentionally exercises the false-positive surface; assertions
// don't check that this case is filtered (V0 limitation).
func FakeUse() error {
	c := &FakeClient{}
	return c.Call("X.Y", nil, nil)
}
