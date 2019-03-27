package rebalancer

import (
	rebalancercache "github.com/cofyc/k8s-rebalancer/pkg/rebalancer/cache"
	"k8s.io/api/core/v1"
	nodeutil "k8s.io/kubernetes/pkg/api/v1/node"
	v1helper "k8s.io/kubernetes/pkg/apis/core/v1/helper"
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

/// podToleratesNodeTaints checks if a pod tolerations can tolerate the node taints
func podToleratesNodeTaints(pod *v1.Pod, node *v1.Node) bool {
	return podToleratesNodeTaintsWithFilter(pod, node, func(t *v1.Taint) bool {
		// PodToleratesNodeTaints is only interested in NoSchedule and NoExecute taints.
		return t.Effect == v1.TaintEffectNoSchedule || t.Effect == v1.TaintEffectNoExecute
	})
}

func podToleratesNodeTaintsWithFilter(pod *v1.Pod, node *v1.Node, filter func(t *v1.Taint) bool) bool {
	if node.Spec.Taints == nil || len(node.Spec.Taints) == 0 {
		return true
	}
	taints := node.Spec.Taints
	if v1helper.TolerationsTolerateTaintsWithFilter(pod.Spec.Tolerations, taints, filter) {
		return true
	}
	return false
}
