package cache

import (
	"k8s.io/api/core/v1"
)

// Interface represents cache interface.
type Interface interface {
	AddNode(node *v1.Node)
	UpdateNode(oldNode, newNode *v1.Node) error
	DeleteNode(node *v1.Node) error
	AddPod(pod *v1.Pod)
	UpdatePod(oldPod, newPod *v1.Pod) error
	DeletePod(pod *v1.Pod) error
	// Snapshot takes a snapshot on current cache
	Snapshot() *Snapshot
}
