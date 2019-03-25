package cache

import (
	"fmt"
	"sync"

	"k8s.io/api/core/v1"
)

type rebalanceCache struct {
	sync.RWMutex
	nodes map[string]*NodeInfo
}

// NewRebalancerCache creates a rebalancer cache.
func NewRebalancerCache() Interface {
	return &rebalanceCache{
		nodes: make(map[string]*NodeInfo),
	}
}

func (rc *rebalanceCache) AddNode(node *v1.Node) {
	rc.Lock()
	defer rc.Unlock()
	n, ok := rc.nodes[node.Name]
	if !ok {
		n = newNodeInfo()
		rc.nodes[node.Name] = n
	}
	n.SetNode(node)
}

func (rc *rebalanceCache) UpdateNode(oldNode, newNode *v1.Node) error {
	rc.Lock()
	defer rc.Unlock()
	n, ok := rc.nodes[newNode.Name]
	if !ok {
		n = newNodeInfo()
		rc.nodes[newNode.Name] = n
	}
	n.SetNode(newNode)
	return nil
}

func (rc *rebalanceCache) DeleteNode(node *v1.Node) error {
	rc.Lock()
	defer rc.Unlock()
	_, ok := rc.nodes[node.Name]
	if !ok {
		return fmt.Errorf("node %v is not found", node.Name)
	}
	delete(rc.nodes, node.Name)
	return nil
}

func (rc *rebalanceCache) AddPod(pod *v1.Pod) {
	rc.Lock()
	defer rc.Unlock()
	rc.addPod(pod)
}

func (rc *rebalanceCache) addPod(pod *v1.Pod) {
	n, ok := rc.nodes[pod.Spec.NodeName]
	if !ok {
		n = newNodeInfo()
		rc.nodes[pod.Spec.NodeName] = n
	}
	n.AddPod(pod)
}

func (rc *rebalanceCache) removePod(pod *v1.Pod) error {
	n, ok := rc.nodes[pod.Spec.NodeName]
	if !ok {
		n = newNodeInfo()
		rc.nodes[pod.Spec.NodeName] = n
	}
	if err := n.RemovePod(pod); err != nil {
		return err
	}
	return nil
}

func (rc *rebalanceCache) UpdatePod(oldPod, newPod *v1.Pod) error {
	rc.Lock()
	defer rc.Unlock()
	if err := rc.removePod(oldPod); err != nil {
		return err
	}
	rc.addPod(newPod)
	return nil
}

func (rc *rebalanceCache) DeletePod(pod *v1.Pod) error {
	rc.Lock()
	defer rc.Unlock()
	if err := rc.removePod(pod); err != nil {
		return err
	}
	return nil
}

func (rc *rebalanceCache) Snapshot() *Snapshot {
	rc.RLock()
	defer rc.RUnlock()
	nodes := make(map[string]*NodeInfo, len(rc.nodes))
	for k, v := range rc.nodes {
		nodes[k] = v.Clone()
	}
	return &Snapshot{
		Nodes: nodes,
	}
}

// Snapshot represents a snapshot of cache.
type Snapshot struct {
	Nodes map[string]*NodeInfo
}
