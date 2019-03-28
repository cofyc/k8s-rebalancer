# k8s-rebalancer

[![Build Status](https://travis-ci.com/cofyc/k8s-rebalancer.svg?token=d2ZWjsd7VLnLR8jqd2ay&branch=master)](https://travis-ci.com/cofyc/k8s-rebalancer)
[![Docker Repository on Quay](https://quay.io/repository/cofyc/k8s-rebalancer/status "Docker Repository on Quay")](https://quay.io/repository/cofyc/k8s-rebalancer)

This is a simple kubernetes controller which rebalances pods across nodes in
cluster.

## Workflow

-> preflight checks

-> snapshot nodes and pods

-> evaluating

Find at most `maxUnscheduledPodsAllowedInCluster` pods that are able to be scheduled to
low scored nodes from high scored nodes. Currently, we only takes CPU/Memory
requested resources into account. 

It is not possible to reschedule assigned scheduled pods, pods which are going
to be drained must be able to be recreated, for example pods managed by
ReplicaSet.

By default, we only drain pods managed by ReplicaSet and ReplicationController.

For each ReplicaSet/ReplicationController, we only drain one pod for each
controller at one time for now.

For simplicity, in current version, we ignore following pods:

  - pods using PVCs, because PV may have node affinity.
  - pods using node affinity
  - pods using node selector
	
-> draining pods

works like `kubectl drain`

## How to calculate resource usage score

```
score := (mostRequestedScore(requested.CPU, allocatable.CPU) + mostRequestedScore(requested.Memory, allocatable.Memory)) / 2
mostRequestedScore(requested, allocatable) := requested * 10 / allocatable
```

This is same as kubernetes ["MostResourceAllocation" priority](https://github.com/kubernetes/kubernetes/blob/055061637a465fd2333431f2361c9f915bdf951d/pkg/scheduler/algorithm/priorities/most_requested.go#L34).

## In future

- add e2e tests
- support draining pods using PVCs
- evaluate more scheduling criteria
- check `rollingUpdate` strategy of deployment/statefulset controllers, then it
  is possible to drain more pods under same controller at same time
- rebalance resource usage between CPU and memory
- rebalance based on real resource usage on nodes
- metrics, e.g. total drained pods, draining pods etc.

## Known issues

- Burstable/BestEffort pods may consume resources then requested, if there are
  many Burstable/BestEffort in cluster, it is impossible to balance real
  resource usage across nodes.
- Imbalance between CPU and Memory is not possible to be mitigated for now,
	e.g. given two nodes A (cpu requested %  0.1, mem requested %d 0.9) and B (mem
	requested % 0.9, cpu requested % 0.1), they have same score
