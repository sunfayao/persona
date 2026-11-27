package node

func watched(n *Node, id string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	_, ok := n.watches[id]
	return ok
}
