package distributed_fixture

// EchoArgs is the request type for the EchoService.Echo JSON-RPC handler.
// Expected: NodeMessageType qname="distributed_fixture.EchoArgs".
type EchoArgs struct {
	Message string
}

// EchoReply is the response type — distributed pass doesn't emit a node for
// it (only the args type drives handles_message).
type EchoReply struct {
	Echo string
}

// EchoService implements one JSON-RPC handler.
type EchoService struct{}

// Echo matches the net/rpc handler signature `func (T) Method(args A, reply *R) error`.
// Expected:
//
//	NodeMessageType qname="distributed_fixture.EchoArgs"
//	handles_message(EchoService.Echo -> distributed_fixture.EchoArgs)
func (s *EchoService) Echo(args EchoArgs, reply *EchoReply) error {
	reply.Echo = args.Message
	return nil
}

// NotJSONRPC has the wrong signature — second arg is not a pointer.
// Expected: NO handles_message edge.
func (s *EchoService) NotJSONRPC(args EchoArgs, reply EchoReply) error {
	return nil
}

// AlsoNotJSONRPC has the wrong return type.
// Expected: NO handles_message edge.
func (s *EchoService) AlsoNotJSONRPC(args EchoArgs, reply *EchoReply) bool {
	return false
}

// FreeFunctionEcho is a free function with the right shape but no receiver.
// Net/rpc requires methods on registered types — expected: NO edge.
func FreeFunctionEcho(args EchoArgs, reply *EchoReply) error {
	return nil
}
