package cache

import (
	"errors"

	"k8s.io/api/core/v1"
	"k8s.io/klog"
	schedulercache "k8s.io/kubernetes/pkg/scheduler/cache"
)

// NodeInfo is node level aggregated information.
// modifed from k8s.io/kubernetes/pkg/scheduler/schedulercache.NodeInfo@release-1.10
type NodeInfo struct {
	node *v1.Node
	pods []*v1.Pod

	requestedResource   *schedulercache.Resource
	allocatableResource *schedulercache.Resource
}

func newNodeInfo() *NodeInfo {
	return &NodeInfo{
		requestedResource:   &schedulercache.Resource{},
		allocatableResource: &schedulercache.Resource{},
	}
}

// SetNode sets node onto node info.
func (n *NodeInfo) SetNode(node *v1.Node) {
	n.node = node
	n.allocatableResource = schedulercache.NewResource(node.Status.Allocatable)
}

func calculateResource(pod *v1.Pod) (res schedulercache.Resource) {
	resPtr := &res
	for _, c := range pod.Spec.Containers {
		resPtr.Add(c.Resources.Requests)
	}
	return
}

// AddPod adds pod.
func (n *NodeInfo) AddPod(pod *v1.Pod) {
	res := calculateResource(pod)
	n.requestedResource.MilliCPU += res.MilliCPU
	n.requestedResource.Memory += res.Memory
	n.pods = append(n.pods, pod)
}

// RemovePod removes pod.
func (n *NodeInfo) RemovePod(pod *v1.Pod) error {
	k1, err := getPodKey(pod)
	if err != nil {
		return err
	}
	for i := range n.pods {
		k2, err := getPodKey(n.pods[i])
		if err != nil {
			klog.Errorf("Cannot get pod key, err: %v", err)
			continue
		}
		if k1 == k2 {
			n.pods[i] = n.pods[len(n.pods)-1]
			n.pods = n.pods[:len(n.pods)-1]
			res := calculateResource(pod)
			n.requestedResource.MilliCPU -= res.MilliCPU
			n.requestedResource.Memory -= res.Memory
			return nil
		}
	}
	return nil
}

// Clone clones a copy.
func (n *NodeInfo) Clone() *NodeInfo {
	clone := &NodeInfo{
		node:                n.node,
		requestedResource:   n.requestedResource.Clone(),
		allocatableResource: n.allocatableResource.Clone(),
	}
	if len(n.pods) > 0 {
		clone.pods = append([]*v1.Pod{}, n.pods...)
	}
	return clone
}

// Pods returns pods in node info.
func (n *NodeInfo) Pods() []*v1.Pod {
	return n.pods
}

// Node returns node in node info.
func (n *NodeInfo) Node() *v1.Node {
	return n.node
}

const (
	// MaxPriority represents max priority.
	MaxPriority = 10
)

func mostRequestedScore(requested, capacity int64) int64 {
	if capacity == 0 {
		return 0
	}
	if requested > capacity {
		return 0
	}

	return (requested * MaxPriority) / capacity
}

// Score calculate requested source score which is in [0, MaxPriority].
// This is same as kubernetes ["MostResourceAllocation" priority](https://github.com/kubernetes/kubernetes/blob/055061637a465fd2333431f2361c9f915bdf951d/pkg/scheduler/algorithm/priorities/most_requested.go#L34).
func (n *NodeInfo) Score() int64 {
	return (mostRequestedScore(n.requestedResource.MilliCPU, n.allocatableResource.MilliCPU) +
		mostRequestedScore(n.requestedResource.Memory, n.allocatableResource.Memory)) / 2
}

var (
	emptyResource = schedulercache.Resource{}
)

// RequestedResource returns requested resources.
func (n *NodeInfo) RequestedResource() schedulercache.Resource {
	if n == nil {
		return emptyResource
	}
	return *n.requestedResource
}

// AllocatableResource returns allocatable resources.
func (n *NodeInfo) AllocatableResource() schedulercache.Resource {
	if n == nil {
		return emptyResource
	}
	return *n.allocatableResource
}

// getPodKey returns the string key of a pod.
func getPodKey(pod *v1.Pod) (string, error) {
	uid := string(pod.UID)
	if len(uid) == 0 {
		return "", errors.New("Cannot get cache key for pod with empty UID")
	}
	return uid, nil
}
