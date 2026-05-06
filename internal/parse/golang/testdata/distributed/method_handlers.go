package distributed_fixture

import "net/http"

// MethodServer exercises the listens_on path with method-receiver
// handlers. Earlier this case produced dangling listens_on edges
// because idForFunc computed an ID from fn.Pos() (= method NAME
// offset), while visitFuncDecl in declarations.go uses the
// FuncDecl Pos (= `func` keyword offset). For methods the two
// positions differ by the receiver-clause width, so the edge
// pointed at a non-existent ID. The fix is to look up by qname
// against v.nodes, which already holds the correctly-emitted
// Method node by the time emitDistributedDecls runs.
type MethodServer struct {
	mux *http.ServeMux
}

func (m *MethodServer) handleA(w http.ResponseWriter, r *http.Request) {}
func (m *MethodServer) handleB(w http.ResponseWriter, r *http.Request) {}

func (m *MethodServer) Register() {
	m.mux.HandleFunc("/method-a", m.handleA)
	m.mux.HandleFunc("/method-b", m.handleB)
}
