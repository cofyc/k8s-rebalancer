package rebalancer

import (
	rebalancercache "github.com/cofyc/k8s-rebalancer/pkg/rebalancer/cache"
	"k8s.io/api/core/v1"
	nodeutil "k8s.io/kubernetes/pkg/api/v1/node"
	schedulerutils "k8s.io/kubernetes/pkg/scheduler/util"
)

type nodePodPair struct {
	nodeInfo *rebalancercache.NodeInfo
	pod      *v1.Pod
}

type sortByPriority []*nodePodPair

func (p sortByPriority) Len() int { return len(p) }

func (p sortByPriority) Less(i, j int) bool {
	priority1 := schedulerutils.GetPodPriority(p[i].pod)
	priority2 := schedulerutils.GetPodPriority(p[j].pod)
	return priority1-priority2 < 0
}

func (p sortByPriority) Swap(i, j int) { p[i], p[j] = p[j], p[i] }

func isNodeReady(node *v1.Node) bool {
	_, condition := nodeutil.GetNodeCondition(&node.Status, v1.NodeReady)
	return condition.Status == v1.ConditionTrue
}
