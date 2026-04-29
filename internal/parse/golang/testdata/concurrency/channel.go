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
