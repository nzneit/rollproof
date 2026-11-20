package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"time"
	"unicode"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"

	"rollproof/internal/cluster"
	"rollproof/internal/config"
	"rollproof/internal/deploy"
	"rollproof/internal/images"
	"rollproof/internal/probe"
	"rollproof/internal/report"
	"rollproof/internal/run"
	"rollproof/internal/verdict"
	"rollproof/internal/watch"
)

// realUnits joins the real units into the steps of a run. keep is
// --keep-cluster: a cluster that failed to come up is then kept too.
func realUnits(keep bool) run.Units {
	w := &wiring{}
	return run.Units{
		Load: func(path string) (*config.Config, error) {
			return config.Load(path, os.LookupEnv)
		},
		Check: w.check,
		Create: func(ctx context.Context, cfg *config.Config, name string, evidence func(string) (io.WriteCloser, error)) (run.Cluster, error) {
			return w.create(ctx, cfg, name, evidence, keep)
		},
		Clock: realClock{},
	}
}

// wiring is what one run's real units share: the engine that Check found.
type wiring struct{ engine cluster.Engine }

// check rejects a custom probe's env that a pod cannot hold, before a cluster
// exists, finds the container engine, which Create then uses, and checks that
// Helm is on the PATH when the configuration uses it.
func (w *wiring) check(ctx context.Context, cfg *config.Config) error {
	if cu := cfg.Probe.Custom; cu != nil {
		if _, err := customEnv(cu.Env); err != nil {
			return err
		}
	}
	e, err := cluster.CheckRunner(ctx)
	if err != nil {
		return err
	}
	w.engine = e
	if cfg.Deploy.Helm != nil {
		return deploy.CheckHelm()
	}
	return nil
}

// create makes the run's cluster and its client, and hands both to a session.
func (w *wiring) create(ctx context.Context, cfg *config.Config, name string, evidence func(string) (io.WriteCloser, error), keep bool) (run.Cluster, error) {
	n, err := cluster.Name(cfg.Path, name)
	if err != nil {
		return nil, err
	}
	c, err := cluster.Create(ctx, cluster.Spec{
		Name:          n,
		RunDir:        cluster.RunDir(n),
		NodeImage:     cfg.Cluster.NodeImage,
		RegistryImage: cfg.Cluster.RegistryImage,
		Workers:       cfg.Cluster.Workers,
		Namespace:     cfg.Deploy.Namespace,
		Timeout:       cfg.Limits.Timeout,
		Keep:          keep,
		Evidence:      evidence,
	}, w.engine)
	if err != nil {
		return nil, err
	}
	client, err := c.Client()
	if err != nil {
		// run deletes no cluster whose Create failed, so this one is deleted here,
		// unless it is kept or the run's context has ended (section 9, "Name and
		// lifetime"). It leaves its logs in the evidence first.
		return nil, failedClientError(ctx, keep, c.Kept(), c.CollectLogs, c.Delete, err)
	}
	return &session{c: c, client: client, cfg: cfg, evidence: evidence}, nil
}

// failedClientError is the error of a cluster that was made but whose client could not be: before
// it keeps or deletes the cluster it takes the cluster's logs with collect, unless ctx has ended,
// and then the cluster is deleted unless it is kept or ctx has ended; the text says so when the
// delete fails too. A collection's error adds the sentence LogsNotCollected and its error to the
// text. A kept cluster's text ends with kept, the sentence its Kept gives. It follows
// internal/cluster's own clean-up after a failed step.
func failedClientError(ctx context.Context, keep bool, kept string, collect, del func(context.Context) error, clientErr error) error {
	c := verdict.Condition{Code: verdict.ClusterFailed,
		Text: "the cluster's API server cannot be reached: " + clientErr.Error()}
	if ctx.Err() == nil {
		if cerr := collect(ctx); cerr != nil {
			c.Text += cluster.LogsNotCollected + cerr.Error()
		}
	}
	if keep {
		c.Text += ". " + capitalised(kept)
		return c
	}
	if ctx.Err() != nil {
		return c
	}
	if err := del(ctx); err != nil {
		c.Text += cluster.LeftBehind
		return errors.Join(c, fmt.Errorf("the clean-up after it failed: %w", err))
	}
	return c
}

// storeFailed is the error of images.Store, after the cluster's logs were taken with collect for a
// cluster-failed error while ctx has not ended; a collection's error adds the sentence
// LogsNotCollected and its error to the condition's text. Any other error is returned as it is,
// and collect is not called.
func storeFailed(ctx context.Context, err error, collect func(context.Context) error) error {
	c := verdict.AsCondition(err)
	if c.Code != verdict.ClusterFailed || ctx.Err() != nil {
		return err
	}
	if cerr := collect(ctx); cerr != nil {
		c.Text += cluster.LogsNotCollected + cerr.Error()
		return c
	}
	return err
}

// capitalised returns s with its first letter upper-cased, as internal/cluster's sentence is when
// it follows a full stop.
func capitalised(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// session is a created cluster as run.Cluster: each method calls one unit.
type session struct {
	c        *cluster.Cluster
	client   kubernetes.Interface
	cfg      *config.Config
	evidence func(string) (io.WriteCloser, error)
	runner   *deploy.OSRunner // the Deployer's runner, for EndCommands
}

var _ run.Cluster = (*session)(nil)

// Store packages the tool's own binary as the probe's image and puts the
// configuration's images in place. A cluster-failed failure of the store takes
// the cluster's logs into the evidence before the error is returned; other
// failures are returned as they are.
func (s *session) Store(ctx context.Context) (string, map[string]config.ImageRef, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", nil, verdict.Condition{Code: verdict.InternalError,
			Text: "the tool's own binary cannot be found: " + err.Error()}
	}
	stored, err := images.Store(ctx, s.c, exe, s.cfg.Images, s.cfg.Limits.Timeout)
	if err != nil {
		return "", nil, storeFailed(ctx, err, s.c.CollectLogs)
	}
	return stored.Probe.Ref(), stored.Images, nil
}

// Deployer makes the run's command runner and the driver that the
// configuration names: the commands driver for deploy.commands, bounded by
// limits.timeout, and the Helm driver for deploy.helm, with or without a
// previous release. Both run on the one runner that Setup and EndCommands use,
// which it keeps so that EndCommands can close it. It also returns the names of
// the variables taken out of the commands' environment.
func (s *session) Deployer(cfg *config.Config) (deploy.Deployer, []string, error) {
	runner := deploy.NewRunner(deploy.RunnerOptions{
		Dir:        cfg.Dir,
		Kubeconfig: s.c.Kubeconfig,
		Environ:    os.Environ(),
		Evidence: func(n string) (io.WriteCloser, error) {
			return s.evidence("commands/" + n)
		},
	})
	s.runner = runner
	if cfg.Deploy.Commands != nil {
		return deploy.NewCommands(deploy.CommandsOptions{
			Commands: *cfg.Deploy.Commands,
			Timeout:  cfg.Limits.Timeout,
			Runner:   runner,
		}), runner.Removed(), nil
	}
	helm := deploy.NewHelm(deploy.HelmOptions{
		Helm:       *cfg.Deploy.Helm,
		Namespace:  cfg.Deploy.Namespace,
		Kubeconfig: s.c.Kubeconfig,
		Context:    s.c.Context,
		RunDir:     s.c.RunDir,
		Dir:        cfg.Dir,
		Timeout:    cfg.Limits.Timeout,
		Runner:     runner,
	})
	return helm, runner.Removed(), nil
}

// Setup runs the configuration's setup commands in order on the runner that
// Deployer made, so that they lose the same variables and write their evidence
// under commands/ as setup, setup-2 and so on, and returns the Result of the
// first that failed with its index, or a success with the index -1. Deployer
// must have been called first.
func (s *session) Setup(ctx context.Context) (deploy.Result, int) {
	return deploy.Setup(ctx, s.runner, s.cfg.Setup, s.cfg.Limits.Timeout)
}

// Watcher follows the workloads of the test's namespace. The Service whose
// endpoints a transition reads comes later, from UseService.
func (s *session) Watcher(ctx context.Context, cfg *config.Config) (run.Watcher, error) {
	startCtx, cancel := context.WithTimeout(ctx, cfg.Limits.Timeout)
	defer cancel()
	w, err := watch.Start(startCtx, s.client, watch.Options{
		Namespace:      cfg.Deploy.Namespace,
		Selector:       cfg.Deploy.Selector,
		Settle:         cfg.Limits.Settle,
		Timeout:        cfg.Limits.Timeout,
		Workers:        cfg.Cluster.Workers,
		Now:            time.Now,
		Evidence:       s.evidence,
		ProbeNamespace: cluster.ProbeNamespace,
	})
	if err != nil {
		return nil, err
	}
	return w, nil
}

// Target checks that the built-in probe's URL reaches a Service of the cluster.
func (s *session) Target(ctx context.Context, cfg *config.Config) (probe.Target, error) {
	return probe.CheckTarget(ctx, s.client, cfg.Probe.HTTP.URL)
}

// Probe makes the pod of the probe that the configuration names, the built-in
// or a custom one. A custom env that a pod cannot hold is config-rejected.
func (s *session) Probe(cfg *config.Config, probeImage string, raw io.Writer) (probe.Probe, error) {
	o, err := s.probeOptions(cfg, probeImage, raw)
	if err != nil {
		return nil, err
	}
	return probe.NewPod(o), nil
}

// probeOptions are podOptions with what the session adds for both probes: its
// client, limits.timeout for the start, raw for the probe's lines, the clock,
// and the run's evidence, into which the probe writes its pod's evidence under
// the names it gives (probe-pod.json, probe-pod/…).
func (s *session) probeOptions(cfg *config.Config, probeImage string, raw io.Writer) (probe.PodOptions, error) {
	o, err := podOptions(cfg, probeImage)
	if err != nil {
		return probe.PodOptions{}, err
	}
	o.Client, o.StartTimeout, o.Raw, o.Now, o.Evidence = s.client, cfg.Limits.Timeout, raw, time.Now, s.evidence
	return o, nil
}

// customDrain is how long a custom probe gets from SIGTERM to its pod's end (section 6, "Stop").
const customDrain = 10 * time.Second

// podOptions are the image, command, arguments, environment, token and drain of
// the probe's pod. The built-in probe: probeImage, its HTTP arguments, no token,
// a drain of the request timeout plus 5 s. A custom probe: its image as
// resolved, its command and args, its env with valueFrom kept, the namespace's
// default token, a drain of 10 s. An env that a pod cannot hold is
// config-rejected.
func podOptions(cfg *config.Config, probeImage string) (probe.PodOptions, error) {
	if cu := cfg.Probe.Custom; cu != nil {
		env, err := customEnv(cu.Env)
		if err != nil {
			return probe.PodOptions{}, err
		}
		return probe.PodOptions{Image: cu.Image, Command: cu.Command, Args: cu.Args, Env: env,
			ServiceAccountToken: true, Drain: customDrain}, nil
	}
	return probe.PodOptions{
		Image: probeImage,
		Args:  probe.HTTPArgs(httpOptions(cfg.Probe.HTTP)),
		Drain: cfg.Probe.HTTP.Timeout + 5*time.Second,
	}, nil
}

// customEnv is a custom probe's env as a container's, or a config-rejected condition whose text,
// probe.CustomEnv's, names the entry by its index and name and quotes no value.
func customEnv(env []config.EnvVar) ([]corev1.EnvVar, error) {
	out, err := probe.CustomEnv(env)
	if err != nil {
		return nil, verdict.Condition{Code: verdict.ConfigRejected, Text: err.Error()}
	}
	return out, nil
}

// Facts is what the cluster says of itself, for the report.
func (s *session) Facts() report.Cluster {
	return report.Cluster{
		Provider:        "kind",
		ProviderVersion: cluster.KindVersion(),
		Kubernetes:      s.c.KubernetesVersion,
		NodeImage:       s.c.NodeImage,
		Workers:         s.c.Workers,
	}
}

// Kept is the sentence for a kept cluster: its kubeconfig file and context.
func (s *session) Kept() string {
	return s.c.Kept()
}

// EndCommands ends every command that is still being stopped and closes its
// evidence. It does nothing when Deployer was never called.
func (s *session) EndCommands() {
	if s.runner != nil {
		s.runner.Close()
	}
}

// Delete deletes the run's cluster and its registry.
func (s *session) Delete(ctx context.Context) error {
	return s.c.Delete(ctx)
}

// realClock is the run's clock: time.Now and time.After.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// httpOptions are the built-in probe's settings as the probe takes them.
func httpOptions(h *config.HTTP) probe.HTTPOptions {
	return probe.HTTPOptions{
		URL:       h.URL,
		Headers:   maps.Clone(h.Headers),
		Rate:      h.Rate,
		Timeout:   h.Timeout,
		ExpectMin: h.ExpectMin,
		ExpectMax: h.ExpectMax,
	}
}
