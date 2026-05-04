package mutex_fixture

// Channel direction + buffer attribute extraction (B1 phase 3).
// Each `make(chan ...)` should produce a NodeChannel with:
//   - sub_kind = "send" / "recv" / "bidi"
//   - signature with elem type and buffer size

func MakeBuffered() chan int {
	return make(chan int, 10)
}

func MakeUnbuffered() chan string {
	return make(chan string)
}

func MakeSendOnly() chan<- int {
	return make(chan<- int, 5)
}

func MakeRecvOnly() <-chan int {
	return make(<-chan int)
}

// Drives goroutine + channel send/recv edges (regression check that B1
// didn't break A0 baseline).
func GoroutineFanout() {
	ch := make(chan int, 3)
	go func() {
		ch <- 1
	}()
	<-ch
}

// ChannelFlowProducer exercises sends_to edge wiring for a channel parameter.
// Note: `out` is a function parameter, not a make() call — chanVarIDs will
// NOT contain it. The sends_to edge falls back to a CallSite Dst (best-effort).
func ChannelFlowProducer(out chan<- int) {
	out <- 42
}

// ChannelFlowConsumer exercises recvs_from edge wiring for a channel parameter.
// Same limitation as ChannelFlowProducer: parameter channels use CallSite Dst.
func ChannelFlowConsumer(in <-chan int) {
	v := <-in
	_ = v
}

// ChannelFlowCoordinated verifies:
//  1. sends_to from the Goroutine node → Channel node (goroutine body send)
//  2. recvs_from from the parent function → Channel node (parent recv)
func ChannelFlowCoordinated() {
	ch := make(chan int, 1)
	go func() { ch <- 1 }()
	v := <-ch
	_ = v
}
