package controller

import (
	"github.com/appscode/go/types"
	api "github.com/kubedb/apimachinery/apis/kubedb/v1alpha1"
	"github.com/kubedb/apimachinery/client/clientset/versioned/typed/kubedb/v1alpha1/util"
	"github.com/kubedb/apimachinery/pkg/eventer"
	core "k8s.io/api/core/v1"
	kerr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientsetscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/reference"
)

const (
	ConfigFIleSecretSuffix = "-conf"
	ConfigFIleName         = "mongod.conf"
)

var (
	mongodbConf = "null"

//	`# mongod.conf
//
//# for documentation of all options, see:
//#   http://docs.mongodb.org/manual/reference/configuration-options/
//
//# Where and how to store data.
//storage:
//  dbPath: /data/db
//  journal:
//    enabled: true
//#  engine:
//#  mmapv1:
//#  wiredTiger:
//
//# where to write logging data.
//systemLog:
//  destination: file
//  logAppend: true
//  path: /var/log/mongodb/mongod.log
//
//# network interfaces
//net:
//  port: 27017
//  bindIp: 127.0.0.1
//
//
//#processManagement:
//
//#security:
//
//#operationProfiling:
//
//#replication:
//
//#sharding:
//
//## Enterprise-Only Options:
//
//#auditLog:
//
//#snmp:
//`
)

func (c *Controller) ensureConfigMap(mongodb *api.MongoDB) error {
	if mongodb.Spec.ConfigFile == nil {
		configVolumeSource, err := c.createConfigFile(mongodb)
		if err != nil {
			if ref, rerr := reference.GetReference(clientsetscheme.Scheme, mongodb); rerr == nil {
				c.recorder.Eventf(
					ref,
					core.EventTypeWarning,
					eventer.EventReasonFailedToCreate,
					`Failed to create Config file (for mongodb.conf). Reason: %v`,
					err.Error(),
				)
			}
			return err
		}
		ms, _, err := util.PatchMongoDB(c.ExtClient, mongodb, func(in *api.MongoDB) *api.MongoDB {
			in.Spec.ConfigFile = &core.VolumeSource{
				ConfigMap: configVolumeSource,
			}
			return in
		})
		if err != nil {
			if ref, rerr := reference.GetReference(clientsetscheme.Scheme, mongodb); rerr == nil {
				c.recorder.Eventf(
					ref,
					core.EventTypeWarning,
					eventer.EventReasonFailedToUpdate,
					err.Error(),
				)
			}
			return err
		}
		mongodb.Spec.ConfigFile = ms.Spec.ConfigFile
	}
	return nil
}

func (c *Controller) createConfigFile(mongodb *api.MongoDB) (*core.ConfigMapVolumeSource, error) {
	configMapName := mongodb.Name + ConfigFIleSecretSuffix

	configMap := &core.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: mongodb.Namespace,
			Labels:    mongodb.OffshootLabels(),
		},
		Data: map[string]string{
			ConfigFIleName: mongodbConf,
		},
	}

	cfg, err := c.Client.CoreV1().ConfigMaps(mongodb.Namespace).Create(configMap)
	if err != nil && !kerr.IsAlreadyExists(err) {
		return nil, err
	}

	return &core.ConfigMapVolumeSource{
		LocalObjectReference: core.LocalObjectReference{
			Name: cfg.Name,
		},
		DefaultMode: types.Int32P(420),
	}, nil
}
