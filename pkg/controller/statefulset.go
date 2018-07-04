package controller

import (
	"fmt"

	"github.com/appscode/go/log"
	"github.com/appscode/go/types"
	mon_api "github.com/appscode/kube-mon/api"
	"github.com/appscode/kutil"
	app_util "github.com/appscode/kutil/apps/v1"
	core_util "github.com/appscode/kutil/core/v1"
	api "github.com/kubedb/apimachinery/apis/kubedb/v1alpha1"
	"github.com/kubedb/apimachinery/pkg/eventer"
	"github.com/the-redback/go-oneliners"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	kerr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientsetscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/reference"
)

const (
	workDirectoryName = "workdir"
	workDirectoryPath = "/work-dir"

	dataDirectoryName = "datadir"
	dataDirectoryPath = "/data/db"

	configDirectoryName = "config"
	configDirectoryPath = "/data/configdb"

	initialConfigDirectoryName = "configdir"
	initialConfigDirectoryPath = "/configdb-readonly"

	initialKeyDirectoryName = "keydir"
	initialKeyDirectoryPath = "/keydir-readonly"
)

func (c *Controller) ensureStatefulSet(mongodb *api.MongoDB) (kutil.VerbType, error) {
	if err := c.checkStatefulSet(mongodb); err != nil {
		return kutil.VerbUnchanged, err
	}

	// Create statefulSet for MongoDB database
	statefulSet, vt, err := c.createStatefulSet(mongodb)
	if err != nil {
		return kutil.VerbUnchanged, err
	}

	// Check StatefulSet Pod status
	if vt != kutil.VerbUnchanged {
		if err := c.checkStatefulSetPodStatus(statefulSet); err != nil {
			if ref, rerr := reference.GetReference(clientsetscheme.Scheme, mongodb); rerr == nil {
				c.recorder.Eventf(
					ref,
					core.EventTypeWarning,
					eventer.EventReasonFailedToStart,
					`Failed to CreateOrPatch StatefulSet. Reason: %v`,
					err,
				)
			}
			return kutil.VerbUnchanged, err
		}
		if ref, rerr := reference.GetReference(clientsetscheme.Scheme, mongodb); rerr == nil {
			c.recorder.Eventf(
				ref,
				core.EventTypeNormal,
				eventer.EventReasonSuccessful,
				"Successfully %v StatefulSet",
				vt,
			)
		}
	}
	return vt, nil
}

func (c *Controller) checkStatefulSet(mongodb *api.MongoDB) error {
	// SatatefulSet for MongoDB database
	statefulSet, err := c.Client.AppsV1().StatefulSets(mongodb.Namespace).Get(mongodb.OffshootName(), metav1.GetOptions{})
	if err != nil {
		if kerr.IsNotFound(err) {
			return nil
		}
		return err
	}

	if statefulSet.Labels[api.LabelDatabaseKind] != api.ResourceKindMongoDB {
		return fmt.Errorf(`intended statefulSet "%v" already exists`, mongodb.OffshootName())
	}

	return nil
}

func (c *Controller) createStatefulSet(mongodb *api.MongoDB) (*apps.StatefulSet, kutil.VerbType, error) {
	statefulSetMeta := metav1.ObjectMeta{
		Name:      mongodb.OffshootName(),
		Namespace: mongodb.Namespace,
	}

	ref, rerr := reference.GetReference(clientsetscheme.Scheme, mongodb)
	if rerr != nil {
		return nil, kutil.VerbUnchanged, rerr
	}

	return app_util.CreateOrPatchStatefulSet(c.Client, statefulSetMeta, func(in *apps.StatefulSet) *apps.StatefulSet {
		in.ObjectMeta = core_util.EnsureOwnerReference(in.ObjectMeta, ref)
		in.Labels = core_util.UpsertMap(in.Labels, mongodb.StatefulSetLabels())
		in.Annotations = core_util.UpsertMap(in.Annotations, mongodb.StatefulSetAnnotations())

		in.Spec.Replicas = types.Int32P(1)
		in.Spec.ServiceName = c.GoverningService
		in.Spec.Template.Labels = in.Labels
		in.Spec.Selector = &metav1.LabelSelector{
			MatchLabels: in.Labels,
		}

		in.Spec.Template.Spec.Containers = core_util.UpsertContainer(in.Spec.Template.Spec.Containers, core.Container{
			Name:  api.ResourceSingularMongoDB,
			Image: c.docker.GetImageWithTag(mongodb),
			Ports: []core.ContainerPort{
				{
					Name:          "db",
					ContainerPort: 27017,
					Protocol:      core.ProtocolTCP,
				},
			},
			Args: []string{
				"--auth",
				"--config=" + configDirectoryPath + "/mongod.conf",
				"--port=" + string(MongoDbPort),
			},
			Resources: mongodb.Spec.Resources,
		})
		if mongodb.GetMonitoringVendor() == mon_api.VendorPrometheus {
			in.Spec.Template.Spec.Containers = core_util.UpsertContainer(in.Spec.Template.Spec.Containers, core.Container{
				Name: "exporter",
				Args: append([]string{
					"export",
					fmt.Sprintf("--address=:%d", mongodb.Spec.Monitor.Prometheus.Port),
					fmt.Sprintf("--enable-analytics=%v", c.EnableAnalytics),
				}, c.LoggerOptions.ToFlags()...),
				Image: c.docker.GetOperatorImageWithTag(mongodb),
				Ports: []core.ContainerPort{
					{
						Name:          api.PrometheusExporterPortName,
						Protocol:      core.ProtocolTCP,
						ContainerPort: mongodb.Spec.Monitor.Prometheus.Port,
					},
				},
				VolumeMounts: []core.VolumeMount{
					{
						Name:      "db-secret",
						MountPath: ExporterSecretPath,
						ReadOnly:  true,
					},
				},
			})
			in.Spec.Template.Spec.Volumes = core_util.UpsertVolume(
				in.Spec.Template.Spec.Volumes,
				core.Volume{
					Name: "db-secret",
					VolumeSource: core.VolumeSource{
						Secret: &core.SecretVolumeSource{
							SecretName: mongodb.Spec.DatabaseSecret.SecretName,
						},
					},
				},
			)
		}
		// Set Admin Secret as MYSQL_ROOT_PASSWORD env variable
		in = upsertEnv(in, mongodb)
		in = upsertDataVolume(in, mongodb)
		in = upsertInitDataVolume(in, mongodb)
		in = addContainerProbe(in, mongodb)
		in = c.upsertInstallInitContainer(in, mongodb)
		if mongodb.Spec.Init != nil && mongodb.Spec.Init.ScriptSource != nil {
			in = upsertInitScript(in, mongodb.Spec.Init.ScriptSource.VolumeSource)
		}

		oneliners.PrettyJson(in, "before rs args")

		if mongodb.Spec.ClusterMode != nil &&
			mongodb.Spec.ClusterMode.ReplicaSet != nil {
			in = c.upsertRSInitContainer(in, mongodb)
			in = upsertRSDataVolume(in, mongodb)
			in = upsertRSArgs(in, mongodb)
			oneliners.PrettyJson(in, "after rs args")
		}

		in.Spec.Template.Spec.NodeSelector = mongodb.Spec.NodeSelector
		in.Spec.Template.Spec.Affinity = mongodb.Spec.Affinity
		in.Spec.Template.Spec.Tolerations = mongodb.Spec.Tolerations
		in.Spec.Template.Spec.ImagePullSecrets = mongodb.Spec.ImagePullSecrets
		if mongodb.Spec.SchedulerName != "" {
			in.Spec.Template.Spec.SchedulerName = mongodb.Spec.SchedulerName
		}

		in.Spec.UpdateStrategy.Type = apps.RollingUpdateStatefulSetStrategyType
		return in
	})
}

func upsertRSArgs(statefulSet *apps.StatefulSet, mongodb *api.MongoDB) *apps.StatefulSet {
	for i, container := range statefulSet.Spec.Template.Spec.Containers {
		if container.Name == api.ResourceSingularMongoDB {
			args := []string{
				"--replSet=" + mongodb.Spec.ClusterMode.ReplicaSet.Name,
				"--bind_ip=0.0.0.0",
				"--keyFile=" + configDirectoryPath + "/" + KeyForKeyFile,
			}
			statefulSet.Spec.Template.Spec.Containers[i].Command = []string{"mongod"}
			statefulSet.Spec.Template.Spec.Containers[i].Args = append(statefulSet.Spec.Template.Spec.Containers[i].Args, args...)
		}
	}
	return statefulSet
}

func addContainerProbe(statefulSet *apps.StatefulSet, mongodb *api.MongoDB) *apps.StatefulSet {
	for i, container := range statefulSet.Spec.Template.Spec.Containers {
		if container.Name == api.ResourceSingularMongoDB {
			cmd := []string{
				"mongo",
				"--eval",
				"db.adminCommand('ping')",
			}
			statefulSet.Spec.Template.Spec.Containers[i].LivenessProbe = &core.Probe{
				Handler: core.Handler{
					Exec: &core.ExecAction{
						Command: cmd,
					},
				},
				FailureThreshold:    3,
				InitialDelaySeconds: 30,
				PeriodSeconds:       10,
				SuccessThreshold:    1,
				TimeoutSeconds:      5,
			}
			statefulSet.Spec.Template.Spec.Containers[i].ReadinessProbe = &core.Probe{
				Handler: core.Handler{
					Exec: &core.ExecAction{
						Command: cmd,
					},
				},
				FailureThreshold:    3,
				InitialDelaySeconds: 5,
				PeriodSeconds:       10,
				SuccessThreshold:    1,
				TimeoutSeconds:      1,
			}
		}
	}
	return statefulSet
}

// Init container for both ReplicaSet and Standalone instances
func (c *Controller) upsertInstallInitContainer(statefulSet *apps.StatefulSet, mongodb *api.MongoDB) *apps.StatefulSet {
	installContainer := core.Container{
		Name:            "install",
		Image:           c.docker.GetInitImage(),
		ImagePullPolicy: core.PullAlways, //todo: ifNotPresent
		Args:            []string{"--work-dir=/work-dir"},
		VolumeMounts: []core.VolumeMount{
			{
				Name:      workDirectoryName,
				MountPath: workDirectoryPath,
			},
			{
				Name:      initialConfigDirectoryName,
				MountPath: initialConfigDirectoryPath,
			},
			{
				Name:      configDirectoryName,
				MountPath: configDirectoryPath,
			},
		},
	}
	if mongodb.Spec.ClusterMode != nil &&
		mongodb.Spec.ClusterMode.ReplicaSet != nil {
		installContainer.VolumeMounts = append(installContainer.VolumeMounts, core.VolumeMount{
			Name:      initialKeyDirectoryName,
			MountPath: initialKeyDirectoryPath,
		})
	}

	initContainers := statefulSet.Spec.Template.Spec.InitContainers
	statefulSet.Spec.Template.Spec.InitContainers = core_util.UpsertContainer(initContainers, installContainer)
	return statefulSet
}

func (c *Controller) upsertRSInitContainer(statefulSet *apps.StatefulSet, mongodb *api.MongoDB) *apps.StatefulSet {
	bootstrapContainer := core.Container{
		Name:            "bootstrap",
		Image:           c.docker.GetImageWithTag(mongodb),
		ImagePullPolicy: core.PullAlways, //todo: ifNotPresent
		Command:         []string{"/work-dir/peer-finder"},
		Args:            []string{"-on-start=/work-dir/on-start.sh", "-service=" + mongodb.ServiceName()},
		Env: []core.EnvVar{
			{
				Name: "POD_NAMESPACE",
				ValueFrom: &core.EnvVarSource{
					FieldRef: &core.ObjectFieldSelector{
						APIVersion: "v1",
						FieldPath:  "metadata.namespace",
					},
				},
			},
			{
				Name:  "REPLICA_SET",
				Value: mongodb.Spec.ClusterMode.ReplicaSet.Name,
			},
			{
				Name:  "AUTH",
				Value: "true",
			},
			{
				Name: "MONGO_INITDB_ROOT_USERNAME",
				ValueFrom: &core.EnvVarSource{
					SecretKeyRef: &core.SecretKeySelector{
						LocalObjectReference: core.LocalObjectReference{
							Name: mongodb.Spec.DatabaseSecret.SecretName,
						},
						Key: KeyMongoDBUser,
					},
				},
			},
			{
				Name: "MONGO_INITDB_ROOT_PASSWORD",
				ValueFrom: &core.EnvVarSource{
					SecretKeyRef: &core.SecretKeySelector{
						LocalObjectReference: core.LocalObjectReference{
							Name: mongodb.Spec.DatabaseSecret.SecretName,
						},
						Key: KeyMongoDBPassword,
					},
				},
			},
		},
		Ports: []core.ContainerPort{
			{
				Name:          "db",
				ContainerPort: 27017,
				Protocol:      core.ProtocolTCP,
			},
		},
		VolumeMounts: []core.VolumeMount{
			{
				Name:      workDirectoryName,
				MountPath: workDirectoryPath,
			},
			{
				Name:      configDirectoryName,
				MountPath: configDirectoryPath,
			},
			{
				Name:      dataDirectoryName,
				MountPath: dataDirectoryPath,
			},
		},
	}

	initContainers := statefulSet.Spec.Template.Spec.InitContainers
	statefulSet.Spec.Template.Spec.InitContainers = core_util.UpsertContainer(initContainers, bootstrapContainer)
	return statefulSet
}

func upsertDataVolume(statefulSet *apps.StatefulSet, mongodb *api.MongoDB) *apps.StatefulSet {
	for i, container := range statefulSet.Spec.Template.Spec.Containers {
		if container.Name == api.ResourceSingularMongoDB {
			volumeMount := core.VolumeMount{
				Name:      dataDirectoryName,
				MountPath: dataDirectoryPath,
			}
			volumeMounts := container.VolumeMounts
			volumeMounts = core_util.UpsertVolumeMount(volumeMounts, volumeMount)
			statefulSet.Spec.Template.Spec.Containers[i].VolumeMounts = volumeMounts

			pvcSpec := mongodb.Spec.Storage

			if len(pvcSpec.AccessModes) == 0 {
				pvcSpec.AccessModes = []core.PersistentVolumeAccessMode{
					core.ReadWriteOnce,
				}
				log.Infof(`Using "%v" as AccessModes in mongodb.Spec.Storage`, core.ReadWriteOnce)
			}

			volumeClaim := core.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: dataDirectoryName,
				},
				Spec: pvcSpec,
			}
			if pvcSpec.StorageClassName != nil {
				volumeClaim.Annotations = map[string]string{
					"volume.beta.kubernetes.io/storage-class": *pvcSpec.StorageClassName,
				}
			}
			volumeClaims := statefulSet.Spec.VolumeClaimTemplates
			volumeClaims = core_util.UpsertVolumeClaim(volumeClaims, volumeClaim)
			statefulSet.Spec.VolumeClaimTemplates = volumeClaims

			// volume mount for config file
			configVolume := core.VolumeMount{
				Name:      configDirectoryName,
				MountPath: configDirectoryPath,
			}
			volumeMounts = statefulSet.Spec.Template.Spec.Containers[i].VolumeMounts
			statefulSet.Spec.Template.Spec.Containers[i].VolumeMounts = core_util.UpsertVolumeMount(volumeMounts, configVolume)

			break
		}
	}
	return statefulSet
}

func upsertInitDataVolume(statefulSet *apps.StatefulSet, mongodb *api.MongoDB) *apps.StatefulSet {
	initVolumes := []core.Volume{
		{
			Name:         initialConfigDirectoryName,
			VolumeSource: *mongodb.Spec.ConfigFile,
		},
		{
			Name: workDirectoryName,
			VolumeSource: core.VolumeSource{
				EmptyDir: &core.EmptyDirVolumeSource{},
			},
		},
		{
			Name: configDirectoryName,
			VolumeSource: core.VolumeSource{
				EmptyDir: &core.EmptyDirVolumeSource{},
			},
		},
	}

	for _, volume := range initVolumes {
		statefulSet.Spec.Template.Spec.Volumes = core_util.UpsertVolume(statefulSet.Spec.Template.Spec.Volumes, volume)
	}

	return statefulSet
}

func upsertRSDataVolume(statefulSet *apps.StatefulSet, mongodb *api.MongoDB) *apps.StatefulSet {
	rsVolume := core.Volume{

		Name: initialKeyDirectoryName,
		VolumeSource: core.VolumeSource{
			Secret: &core.SecretVolumeSource{
				DefaultMode: types.Int32P(256),
				SecretName:  mongodb.Spec.ClusterMode.ReplicaSet.KeyFileSecret.SecretName,
			},
		},
	}
	volumes := statefulSet.Spec.Template.Spec.Volumes
	statefulSet.Spec.Template.Spec.Volumes = core_util.UpsertVolume(volumes, rsVolume)
	return statefulSet
}

func upsertEnv(statefulSet *apps.StatefulSet, mongodb *api.MongoDB) *apps.StatefulSet {
	envList := []core.EnvVar{
		{
			Name: "MONGO_INITDB_ROOT_USERNAME",
			ValueFrom: &core.EnvVarSource{
				SecretKeyRef: &core.SecretKeySelector{
					LocalObjectReference: core.LocalObjectReference{
						Name: mongodb.Spec.DatabaseSecret.SecretName,
					},
					Key: KeyMongoDBUser,
				},
			},
		},
		{
			Name: "MONGO_INITDB_ROOT_PASSWORD",
			ValueFrom: &core.EnvVarSource{
				SecretKeyRef: &core.SecretKeySelector{
					LocalObjectReference: core.LocalObjectReference{
						Name: mongodb.Spec.DatabaseSecret.SecretName,
					},
					Key: KeyMongoDBPassword,
				},
			},
		},
	}
	for i, container := range statefulSet.Spec.Template.Spec.Containers {
		if container.Name == api.ResourceSingularMongoDB {
			statefulSet.Spec.Template.Spec.Containers[i].Env = core_util.UpsertEnvVars(container.Env, envList...)
			return statefulSet
		}
	}
	return statefulSet
}

func upsertInitScript(statefulSet *apps.StatefulSet, script core.VolumeSource) *apps.StatefulSet {
	for i, container := range statefulSet.Spec.Template.Spec.Containers {
		if container.Name == api.ResourceSingularMongoDB {
			volumeMount := core.VolumeMount{
				Name:      "initial-script",
				MountPath: "/docker-entrypoint-initdb.d",
			}
			statefulSet.Spec.Template.Spec.Containers[i].VolumeMounts = core_util.UpsertVolumeMount(
				container.VolumeMounts,
				volumeMount,
			)

			volume := core.Volume{
				Name:         "initial-script",
				VolumeSource: script,
			}
			statefulSet.Spec.Template.Spec.Volumes = core_util.UpsertVolume(
				statefulSet.Spec.Template.Spec.Volumes,
				volume,
			)
			return statefulSet
		}
	}
	return statefulSet
}

func (c *Controller) checkStatefulSetPodStatus(statefulSet *apps.StatefulSet) error {
	err := core_util.WaitUntilPodRunningBySelector(
		c.Client,
		statefulSet.Namespace,
		statefulSet.Spec.Selector,
		int(types.Int32(statefulSet.Spec.Replicas)),
	)
	if err != nil {
		return err
	}
	return nil
}
