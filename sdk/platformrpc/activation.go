package platformrpc

// Activations tracks the one active platform and any candidate being
// prepared. Call its methods on the host's application thread.
type Activations struct {
	next      uint64
	active    Scope
	candidate Scope
}

func (a *Activations) Prepare(connection string) Scope {
	a.next++
	a.candidate = Scope{Connection: connection, Activation: a.next}
	return a.candidate
}

func (a *Activations) Commit(candidate Scope) bool {
	if candidate == (Scope{}) || candidate != a.candidate {
		return false
	}
	a.active, a.candidate = candidate, Scope{}
	return true
}

func (a *Activations) Revoke(scope Scope) {
	if scope == a.candidate {
		a.candidate = Scope{}
	}
	if scope == a.active {
		a.active = Scope{}
	}
}

// Check authorizes the request in the thread that will read or mutate
// the shared application state. The connection comes from the transport.
func (a *Activations) Check(connection string, scope Scope, write bool) error {
	if scope.Activation != 0 && scope.Connection == connection {
		if scope == a.active || (!write && scope == a.candidate) {
			return nil
		}
		if scope == a.candidate {
			return Failure{Code: "not-active"}
		}
	}
	return Failure{Code: "stale-activation"}
}
