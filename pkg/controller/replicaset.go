package controller

import (
	"github.com/appscode/go/types"
	core_util "github.com/appscode/kutil/core/v1"
	api "github.com/kubedb/apimachinery/apis/kubedb/v1alpha1"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

func upsertRSArgs(statefulSet *apps.StatefulSet, mongodb *api.MongoDB) *apps.StatefulSet {
	for i, container := range statefulSet.Spec.Template.Spec.Containers {
		if container.Name == api.ResourceSingularMongoDB {
			args := sets.NewString(statefulSet.Spec.Template.Spec.Containers[i].Args...)
			args.Insert("--replSet="+mongodb.Spec.ClusterMode.ReplicaSet.Name,
				"--bind_ip=0.0.0.0",
				"--keyFile="+configDirectoryPath+"/"+KeyForKeyFile)
			statefulSet.Spec.Template.Spec.Containers[i].Args = args.List()

			statefulSet.Spec.Template.Spec.Containers[i].Command = []string{
				"mongod",
			}
		}
	}
	return statefulSet
}

func (c *Controller) upsertRSInitContainer(statefulSet *apps.StatefulSet, mongodb *api.MongoDB) *apps.StatefulSet {
	bootstrapContainer := core.Container{
		Name:            InitBootstrapContainerName,
		Image:           c.docker.GetImageWithTag(mongodb),
		ImagePullPolicy: core.PullAlways, //todo: ifNotPresent
		Command:         []string{"/work-dir/peer-finder"},
		Args:            []string{"-on-start=/work-dir/on-start.sh", "-service=" + c.GoverningService},
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
