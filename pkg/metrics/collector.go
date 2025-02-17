package metrics

import (
	"context"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	k8smetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	"github.com/aquasecurity/trivy-operator/pkg/apis/aquasecurity/v1alpha1"
	"github.com/aquasecurity/trivy-operator/pkg/operator/etc"
)

const (
	namespace        = "namespace"
	name             = "name"
	image_registry   = "image_registry"
	image_repository = "image_repository"
	image_tag        = "image_tag"
	image_digest     = "image_digest"
	severity         = "severity"
)

var (
	imageVulnLabels = []string{
		namespace,
		name,
		image_registry,
		image_repository,
		image_tag,
		image_digest,
		severity,
	}
	imageVulnDesc = prometheus.NewDesc(
		prometheus.BuildFQName("trivy", "image", "vulnerabilities"),
		"Number of container image vulnerabilities",
		imageVulnLabels,
		nil,
	)
	imageVulnSeverities = map[string]func(vs v1alpha1.VulnerabilitySummary) int{
		"Critical": func(vs v1alpha1.VulnerabilitySummary) int {
			return vs.CriticalCount
		},
		"High": func(vs v1alpha1.VulnerabilitySummary) int {
			return vs.HighCount
		},
		"Medium": func(vs v1alpha1.VulnerabilitySummary) int {
			return vs.MediumCount
		},
		"Low": func(vs v1alpha1.VulnerabilitySummary) int {
			return vs.LowCount
		},
		"Unknown": func(vs v1alpha1.VulnerabilitySummary) int {
			return vs.UnknownCount
		},
	}
	exposedSecretLabels = []string{
		namespace,
		name,
		image_registry,
		image_repository,
		image_tag,
		image_digest,
		severity,
	}
	exposedSecretDesc = prometheus.NewDesc(
		prometheus.BuildFQName("trivy", "image", "exposedsecrets"),
		"Number of image exposed secrets",
		exposedSecretLabels,
		nil,
	)
	exposedSecretSeverities = map[string]func(vs v1alpha1.ExposedSecretSummary) int{
		"Critical": func(vs v1alpha1.ExposedSecretSummary) int {
			return vs.CriticalCount
		},
		"High": func(vs v1alpha1.ExposedSecretSummary) int {
			return vs.HighCount
		},
		"Medium": func(vs v1alpha1.ExposedSecretSummary) int {
			return vs.MediumCount
		},
		"Low": func(vs v1alpha1.ExposedSecretSummary) int {
			return vs.LowCount
		},
	}
	configAuditLabels = []string{
		namespace,
		name,
		severity,
	}
	configAuditDesc = prometheus.NewDesc(
		prometheus.BuildFQName("trivy", "resource", "configaudits"),
		"Number of failing resource configuration auditing checks",
		configAuditLabels,
		nil,
	)
	configAuditSeverities = map[string]func(vs v1alpha1.ConfigAuditSummary) int{
		"Critical": func(cas v1alpha1.ConfigAuditSummary) int {
			return cas.CriticalCount
		},
		"High": func(cas v1alpha1.ConfigAuditSummary) int {
			return cas.HighCount
		},
		"Medium": func(cas v1alpha1.ConfigAuditSummary) int {
			return cas.MediumCount
		},
		"Low": func(cas v1alpha1.ConfigAuditSummary) int {
			return cas.LowCount
		},
	}
)

// ResourcesMetricsCollector is a custom Prometheus collector that produces
// metrics on-demand from the trivy-operator custom resources. Since these
// resources are already cached by the Kubernetes API client shared with the
// operator, metrics scrapes should never actually hit the API server.
// All resource reads are served from cache, reducing API server load without
// consuming additional cluster resources.
// An alternative (more traditional) approach would be to maintain metrics
// in the internal Prometheus registry on resource reconcile. The collector
// approach was selected in order to avoid potentially stale metrics; i.e.
// the controller would have to reconcile all resources at least once for the
// metrics to be up-to-date, which could take some time in large clusters.
// Also deleting metrics from registry for obsolete/deleted resources is
// challenging without introducing finalizers, which we want to avoid for
// operational reasons.
//
// For more advanced use-cases, and/or very large clusters, this internal
// collector can be disabled and replaced by
// https://github.com/giantswarm/starboard-exporter, which collects trivy
// metrics from a dedicated workload supporting sharding etc.
type ResourcesMetricsCollector struct {
	logr.Logger
	etc.Config
	client.Client
}

func (c *ResourcesMetricsCollector) SetupWithManager(mgr ctrl.Manager) error {
	return mgr.Add(c)
}

func (c ResourcesMetricsCollector) Collect(metrics chan<- prometheus.Metric) {
	ctx := context.Background()

	targetNamespaces := c.Config.GetTargetNamespaces()
	if len(targetNamespaces) == 0 {
		targetNamespaces = append(targetNamespaces, "")
	}
	c.collectVulnerabilityReports(ctx, metrics, targetNamespaces)
	c.collectExposedSecretsReports(ctx, metrics, targetNamespaces)
	c.collectConfigAuditReports(ctx, metrics, targetNamespaces)
}

func (c ResourcesMetricsCollector) collectVulnerabilityReports(ctx context.Context, metrics chan<- prometheus.Metric, targetNamespaces []string) {
	reports := &v1alpha1.VulnerabilityReportList{}
	labelValues := make([]string, 7)
	for _, n := range targetNamespaces {
		if err := c.List(ctx, reports, client.InNamespace(n)); err != nil {
			c.Logger.Error(err, "failed to list vulnerabilityreports from API", "namespace", n)
			continue
		}
		for _, r := range reports.Items {
			labelValues[0] = r.Namespace
			labelValues[1] = r.Name
			labelValues[2] = r.Report.Registry.Server
			labelValues[3] = r.Report.Artifact.Repository
			labelValues[4] = r.Report.Artifact.Tag
			labelValues[5] = r.Report.Artifact.Digest
			for severity, countFn := range imageVulnSeverities {
				labelValues[6] = severity
				count := countFn(r.Report.Summary)
				metrics <- prometheus.MustNewConstMetric(imageVulnDesc, prometheus.GaugeValue, float64(count), labelValues...)
			}
		}
	}
}

func (c ResourcesMetricsCollector) collectExposedSecretsReports(ctx context.Context, metrics chan<- prometheus.Metric, targetNamespaces []string) {
	reports := &v1alpha1.ExposedSecretReportList{}
	labelValues := make([]string, 7)
	for _, n := range targetNamespaces {
		if err := c.List(ctx, reports, client.InNamespace(n)); err != nil {
			c.Logger.Error(err, "failed to list exposedsecretreports from API", "namespace", n)
			continue
		}
		for _, r := range reports.Items {
			labelValues[0] = r.Namespace
			labelValues[1] = r.Name
			labelValues[2] = r.Report.Registry.Server
			labelValues[3] = r.Report.Artifact.Repository
			labelValues[4] = r.Report.Artifact.Tag
			labelValues[5] = r.Report.Artifact.Digest
			for severity, countFn := range exposedSecretSeverities {
				labelValues[6] = severity
				count := countFn(r.Report.Summary)
				metrics <- prometheus.MustNewConstMetric(exposedSecretDesc, prometheus.GaugeValue, float64(count), labelValues...)
			}
		}
	}
}

func (c *ResourcesMetricsCollector) collectConfigAuditReports(ctx context.Context, metrics chan<- prometheus.Metric, targetNamespaces []string) {
	reports := &v1alpha1.ConfigAuditReportList{}
	labelValues := make([]string, 3)
	for _, n := range targetNamespaces {
		if err := c.List(ctx, reports, client.InNamespace(n)); err != nil {
			c.Logger.Error(err, "failed to list configauditreports from API", "namespace", n)
			continue
		}
		for _, r := range reports.Items {
			labelValues[0] = r.Namespace
			labelValues[1] = r.Name
			for severity, countFn := range configAuditSeverities {
				labelValues[2] = severity
				count := countFn(r.Report.Summary)
				metrics <- prometheus.MustNewConstMetric(configAuditDesc, prometheus.GaugeValue, float64(count), labelValues...)
			}
		}
	}
}

func (c ResourcesMetricsCollector) Describe(descs chan<- *prometheus.Desc) {
	descs <- imageVulnDesc
	descs <- configAuditDesc
	descs <- exposedSecretDesc
}

func (c ResourcesMetricsCollector) Start(ctx context.Context) error {
	c.Logger.Info("Registering resources metrics collector")
	if err := k8smetrics.Registry.Register(c); err != nil {
		return err
	}

	// Block until the context is done.
	<-ctx.Done()

	c.Logger.Info("Unregistering resources metrics collector")
	k8smetrics.Registry.Unregister(c)
	return nil
}

func (c ResourcesMetricsCollector) NeedLeaderElection() bool {
	return true
}

// Ensure ResourcesMetricsCollector is leader-election aware
var _ manager.LeaderElectionRunnable = &ResourcesMetricsCollector{}
