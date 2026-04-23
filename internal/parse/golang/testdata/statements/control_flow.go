package cf

func Decide(n int) int {
	if n < 0 { // IfStmt
		return -1 // ReturnStmt
	}
	for i := 0; i < n; i++ { // LoopStmt sub_kind=for
	}
	for k := range []int{1, 2} { // LoopStmt sub_kind=range
		_ = k
	}
	switch n { // SwitchStmt
	case 0:
	}
	ch := make(chan int)    // CallSite for `make`
	go func() { ch <- 1 }() // Goroutine + CallSite + sends_to
	<-ch                    // recvs_from CallSite
	return n                // ReturnStmt
}
