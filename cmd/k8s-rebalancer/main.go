package main

import (
	"context"
	goflag "flag"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/cofyc/k8s-rebalancer/cmd/k8s-rebalancer/config"
	"github.com/cofyc/k8s-rebalancer/cmd/k8s-rebalancer/options"
	"github.com/cofyc/k8s-rebalancer/pkg/rebalancer"
	flag "github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apiserver/pkg/util/logs"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/klog"
	utilflag "k8s.io/kubernetes/pkg/util/flag"
)

// ResyncPeriod returns a function which generates a duration each time it is
// invoked; this is so that multiple controllers don't get into lock-step and all
// hammer the apiserver with list requests simultaneously.
func ResyncPeriod(c *config.CompletedConfig) func() time.Duration {
	return func() time.Duration {
		factor := rand.Float64() + 1
		return time.Duration(float64(c.GenericComponent.MinResyncPeriod.Nanoseconds()) * factor)
	}
}

// Run runs the controller-manager. This should never exit.
func Run(c *config.CompletedConfig) error {
	run := func(ctx context.Context) {
		informerFactory := informers.NewSharedInformerFactory(c.Client, c.GenericComponent.MinResyncPeriod.Duration)
		r, err := rebalancer.NewRebalancer(c.Client, informerFactory)
		if err != nil {
			klog.Fatal(err)
		}
		go r.Run(ctx.Done())
		// Start informers after all event listeners are registered.
		informerFactory.Start(ctx.Done())
		select {}
	}

	if !c.GenericComponent.LeaderElection.LeaderElect {
		run(context.TODO())
		panic("unreachable")
	}

	id, err := os.Hostname()
	if err != nil {
		klog.Fatal(err)
	}

	// add a uniquifier so that two processes on the same host don't accidentally both become active
	id = id + "_" + string(uuid.NewUUID())
	rl, err := resourcelock.New(c.GenericComponent.LeaderElection.ResourceLock,
		"kube-system",
		"k8s-rebalancer",
		c.LeaderElectionClient.CoreV1(),
		resourcelock.ResourceLockConfig{
			Identity:      id,
			EventRecorder: c.EventRecorder,
		})
	if err != nil {
		klog.Fatalf("error creating lock: %v", err)
	}

	leaderelection.RunOrDie(context.TODO(), leaderelection.LeaderElectionConfig{
		Lock:          rl,
		LeaseDuration: c.GenericComponent.LeaderElection.LeaseDuration.Duration,
		RenewDeadline: c.GenericComponent.LeaderElection.RenewDeadline.Duration,
		RetryPeriod:   c.GenericComponent.LeaderElection.RetryPeriod.Duration,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: run,
			OnStoppedLeading: func() {
				klog.Fatalf("leaderelection lost")
			},
		},
	})
	panic("unreachable")
}

func init() {
	rand.Seed(time.Now().UnixNano())
}

func main() {
	logs.InitLogs()
	defer logs.FlushLogs()
	flag.CommandLine.AddGoFlagSet(goflag.CommandLine)
	opts := options.NewK8sRebalancerOptions()
	opts.AddFlags(flag.CommandLine)
	flag.Parse()
	flag.Set("logtostderr", "true")

	// TODO version flag
	// verflag.PrintAndExitIfRequested()
	utilflag.PrintFlags(flag.CommandLine)

	c, err := opts.Config()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	Run(c.Complete())
}
