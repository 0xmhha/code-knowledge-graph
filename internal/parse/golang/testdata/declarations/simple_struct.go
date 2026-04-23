package simple

// Counter holds a tally.
type Counter struct {
	N int
}

// Inc increments the counter.
func (c *Counter) Inc() {
	c.N++
}
