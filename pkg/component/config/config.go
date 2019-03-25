package config

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	apiserverapisconfig "k8s.io/apiserver/pkg/apis/config"
	schedulerapisconfig "k8s.io/kubernetes/pkg/scheduler/apis/config"
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
	LeaderElection apiserverapisconfig.LeaderElectionConfiguration
}

// NewDefaultGenericComponentConfiguration returns default GenericComponentConfiguration.
func NewDefaultGenericComponentConfiguration() GenericComponentConfiguration {
	return GenericComponentConfiguration{
		MinResyncPeriod:         metav1.Duration{Duration: 12 * time.Hour},
		ContentType:             "application/vnd.kubernetes.protobuf",
		KubeAPIQPS:              20,
		KubeAPIBurst:            30,
		ControllerStartInterval: metav1.Duration{Duration: 0 * time.Second},
		LeaderElection:          newDefaultLeaderElectionConfig(),
	}
}

func newDefaultLeaderElectionConfig() apiserverapisconfig.LeaderElectionConfiguration {
	scheme := runtime.NewScheme()
	schedulerapisconfig.AddToScheme(scheme)

	versioned := schedulerapisconfig.KubeSchedulerConfiguration{}
	scheme.Default(&versioned)

	internal := schedulerapisconfig.KubeSchedulerConfiguration{}
	scheme.Convert(&versioned, &internal, nil)
	return internal.LeaderElection.LeaderElectionConfiguration
}
