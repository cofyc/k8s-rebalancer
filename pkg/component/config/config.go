package config

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiserverconfigv1alpha1 "k8s.io/apiserver/pkg/apis/config/v1alpha1"
)

// GenericComponentConfiguration is generic component configuration.
type GenericComponentConfiguration struct {
	// minResyncPeriod is the resync period in reflectors; will be random between
	// minResyncPeriod and 2*minResyncPeriod.
	MinResyncPeriod metav1.Duration
	// contentType is contentType of requests sent to apiserver.
	ContentType string
	// kubeAPIQPS is the QPS to use while talking with kubernetes apiserver.
	KubeAPIQPS float32
	// kubeAPIBurst is the burst to use while talking with kubernetes apiserver.
	KubeAPIBurst int32
	// How long to wait between starting controller managers
	ControllerStartInterval metav1.Duration
	// leaderElection defines the configuration of leader election client.
	LeaderElection apiserverconfigv1alpha1.LeaderElectionConfiguration
}

// NewDefaultGenericComponentConfiguration returns default GenericComponentConfiguration.
func NewDefaultGenericComponentConfiguration() GenericComponentConfiguration {
	c := GenericComponentConfiguration{
		MinResyncPeriod:         metav1.Duration{Duration: 12 * time.Hour},
		ContentType:             "application/vnd.kubernetes.protobuf",
		KubeAPIQPS:              20,
		KubeAPIBurst:            30,
		ControllerStartInterval: metav1.Duration{Duration: 0 * time.Second},
	}
	apiserverconfigv1alpha1.RecommendedDefaultLeaderElectionConfiguration(&c.LeaderElection)
	return c
}
