package controller

import (
	core_util "github.com/appscode/kutil/core/v1"
	api "github.com/kubedb/apimachinery/apis/kubedb/v1alpha1"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	ConfigSourceSecretSuffix = "-conf"
	ConfigSourceName         = "mongod.conf"
)

func (c *Controller) upsertConfigSourceVolume(statefulSet *apps.StatefulSet, mongodb *api.MongoDB) *apps.StatefulSet {

	for i, container := range statefulSet.Spec.Template.Spec.Containers {
		if container.Name == api.ResourceSingularMongoDB {
			args := sets.NewString(statefulSet.Spec.Template.Spec.Containers[i].Args...)
			args.Insert("--config=", configDirectoryPath+"/mongod.conf")
			statefulSet.Spec.Template.Spec.Containers[i].Args = args.List()
		}
	}

	for i, container := range statefulSet.Spec.Template.Spec.InitContainers {
		if container.Name == InitInstallContainerName {

			volumeMounts := []core.VolumeMount{
				{
					Name:      initialConfigDirectoryName,
					MountPath: initialConfigDirectoryPath,
				},
				{
					Name:      configDirectoryName,
					MountPath: configDirectoryPath,
				},
			}
			statefulSet.Spec.Template.Spec.InitContainers[i].VolumeMounts = core_util.UpsertVolumeMount(
				statefulSet.Spec.Template.Spec.InitContainers[i].VolumeMounts, volumeMounts...)
		}
	}

	volumes := []core.Volume{
		{
			Name:         initialConfigDirectoryName,
			VolumeSource: *mongodb.Spec.ConfigSource,
		},
		{
			Name: configDirectoryName,
			VolumeSource: core.VolumeSource{
				EmptyDir: &core.EmptyDirVolumeSource{},
			},
		},
	}
	statefulSet.Spec.Template.Spec.Volumes = core_util.UpsertVolume(statefulSet.Spec.Template.Spec.Volumes, volumes...)

	return statefulSet
}
