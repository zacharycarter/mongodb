package dormantdatabase

import (
	"fmt"
	"sync"

	hookapi "github.com/appscode/kubernetes-webhook-util/admission/v1beta1"
	core_util "github.com/appscode/kutil/core/v1"
	meta_util "github.com/appscode/kutil/meta"
	api "github.com/kubedb/apimachinery/apis/kubedb/v1alpha1"
	cs "github.com/kubedb/apimachinery/client/clientset/versioned"
	"github.com/kubedb/apimachinery/client/clientset/versioned/typed/kubedb/v1alpha1/util"
	plugin "github.com/kubedb/apimachinery/pkg/admission"
	admission "k8s.io/api/admission/v1beta1"
	coreV1 "k8s.io/api/core/v1"
	kerr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	clientsetscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/reference"
)

type DormantDatabaseValidator struct {
	client      kubernetes.Interface
	extClient   cs.Interface
	lock        sync.RWMutex
	initialized bool
}

var _ hookapi.AdmissionHook = &DormantDatabaseValidator{}

func (a *DormantDatabaseValidator) Resource() (plural schema.GroupVersionResource, singular string) {
	return schema.GroupVersionResource{
			Group:    "validators.kubedb.com",
			Version:  "v1alpha1",
			Resource: "dormantdatabases",
		},
		"dormantdatabase"
}

func (a *DormantDatabaseValidator) Initialize(config *rest.Config, stopCh <-chan struct{}) error {
	a.lock.Lock()
	defer a.lock.Unlock()

	a.initialized = true

	var err error
	if a.client, err = kubernetes.NewForConfig(config); err != nil {
		return err
	}
	if a.extClient, err = cs.NewForConfig(config); err != nil {
		return err
	}
	return err
}

func (a *DormantDatabaseValidator) Admit(req *admission.AdmissionRequest) *admission.AdmissionResponse {
	status := &admission.AdmissionResponse{}

	// No validation on CREATE
	if (req.Operation != admission.Update && req.Operation != admission.Delete) ||
		len(req.SubResource) != 0 ||
		req.Kind.Group != api.SchemeGroupVersion.Group ||
		req.Kind.Kind != api.ResourceKindDormantDatabase {
		status.Allowed = true
		return status
	}

	a.lock.RLock()
	defer a.lock.RUnlock()
	if !a.initialized {
		return hookapi.StatusUninitialized()
	}

	switch req.Operation {
	case admission.Delete:
		if req.Name != "" {
			// req.Object.Raw = nil, so read from kubernetes
			obj, err := a.extClient.KubedbV1alpha1().DormantDatabases(req.Namespace).Get(req.Name, metav1.GetOptions{})
			if err != nil && !kerr.IsNotFound(err) {
				return hookapi.StatusInternalServerError(err)
			} else if kerr.IsNotFound(err) {
				break
			}
			if err := a.handleOwnerReferences(obj); err != nil {
				return hookapi.StatusInternalServerError(err)
			}
		}
	case admission.Update:
		// validate the operation made by User
		obj, err := meta_util.UnmarshalFromJSON(req.Object.Raw, api.SchemeGroupVersion)
		if err != nil {
			return hookapi.StatusBadRequest(err)
		}
		OldObj, err := meta_util.UnmarshalFromJSON(req.OldObject.Raw, api.SchemeGroupVersion)
		if err != nil {
			return hookapi.StatusBadRequest(err)
		}
		if err := plugin.ValidateUpdate(obj, OldObj, req.Kind.Kind); err != nil {
			return hookapi.StatusBadRequest(fmt.Errorf("%v", err))
		}
	}

	status.Allowed = true
	return status
}

func (a *DormantDatabaseValidator) handleOwnerReferences(dormantDatabase *api.DormantDatabase) error {
	if dormantDatabase.Spec.WipeOut {
		if err := a.setOwnerReferenceToObjects(dormantDatabase); err != nil {
			return err
		}
	} else {
		if err := a.removeOwnerReferenceFromObjects(dormantDatabase); err != nil {
			return err
		}
	}
	return nil
}

func (a *DormantDatabaseValidator) setOwnerReferenceToObjects(dormantDatabase *api.DormantDatabase) error {
	// Get LabelSelector for Other Components first
	dbKind, err := meta_util.GetStringValue(dormantDatabase.ObjectMeta.Labels, api.LabelDatabaseKind)
	if err != nil {
		return err
	}
	labelMap := map[string]string{
		api.LabelDatabaseName: dormantDatabase.Name,
		api.LabelDatabaseKind: dbKind,
	}
	labelSelector := labels.SelectorFromSet(labelMap)

	// Get object reference of dormant database
	ref, rerr := reference.GetReference(clientsetscheme.Scheme, dormantDatabase)
	if rerr != nil {
		return rerr
	}

	if dbKind != api.ResourceKindMemcached && dbKind != api.ResourceKindRedis {
		// Set Owner Reference of Snapshots to this Dormant Database Object
		snapshotList, err := a.extClient.KubedbV1alpha1().Snapshots(dormantDatabase.Namespace).List(
			metav1.ListOptions{
				LabelSelector: labelSelector.String(),
			},
		)
		if err != nil {
			return err
		}
		for _, snapshot := range snapshotList.Items {
			if _, _, err := util.PatchSnapshot(a.extClient.KubedbV1alpha1(), &snapshot, func(in *api.Snapshot) *api.Snapshot {
				in.ObjectMeta = core_util.EnsureOwnerReference(in.ObjectMeta, ref)
				return in
			}); err != nil {
				return err
			}
		}
	}

	// Set Owner Reference of PVC to this Dormant Database Object
	pvcList, err := a.client.CoreV1().PersistentVolumeClaims(dormantDatabase.Namespace).List(
		metav1.ListOptions{
			LabelSelector: labelSelector.String(),
		},
	)
	if err != nil {
		return err
	}
	for _, pvc := range pvcList.Items {
		if _, _, err := core_util.PatchPVC(a.client, &pvc, func(in *coreV1.PersistentVolumeClaim) *coreV1.PersistentVolumeClaim {
			in.ObjectMeta = core_util.EnsureOwnerReference(in.ObjectMeta, ref)
			return in
		}); err != nil {
			return err
		}
	}

	// Set Owner Reference of Secret to this Dormant Database Object
	// only if the secret is not used by other xDB (Similar kind) or DormantDB
	secretVolList := getDatabaseSecretName(dormantDatabase, dbKind)
	for _, secretVolSrc := range secretVolList {
		if secretVolSrc == nil {
			continue
		}
		if err := a.sterilizeSecrets(dormantDatabase, secretVolSrc); err != nil {
			return err
		}
	}

	// Set owner Reference to ConfigMap
	cfgList, err := a.client.CoreV1().ConfigMaps(dormantDatabase.Namespace).List(
		metav1.ListOptions{
			LabelSelector: labelSelector.String(),
		},
	)
	if err != nil {
		return err
	}
	for _, cfg := range cfgList.Items {
		if _, _, err := core_util.PatchConfigMap(a.client, &cfg, func(in *coreV1.ConfigMap) *coreV1.ConfigMap {
			in.ObjectMeta = core_util.EnsureOwnerReference(in.ObjectMeta, ref)
			return in
		}); err != nil {
			return err
		}
	}

	return nil
}

func (a *DormantDatabaseValidator) removeOwnerReferenceFromObjects(dormantDatabase *api.DormantDatabase) error {
	// First, Get LabelSelector for Other Components
	dbKind, err := meta_util.GetStringValue(dormantDatabase.ObjectMeta.Labels, api.LabelDatabaseKind)
	if err != nil {
		return err
	}
	labelMap := map[string]string{
		api.LabelDatabaseName: dormantDatabase.Name,
		api.LabelDatabaseKind: dbKind,
	}
	labelSelector := labels.SelectorFromSet(labelMap)

	// Get object reference of dormant database
	ref, rerr := reference.GetReference(clientsetscheme.Scheme, dormantDatabase)
	if rerr != nil {
		return rerr
	}

	// Remove Owner Reference of Snapshots
	if dbKind != api.ResourceKindMemcached && dbKind != api.ResourceKindRedis {
		snapshotList, err := a.extClient.KubedbV1alpha1().Snapshots(dormantDatabase.Namespace).List(
			metav1.ListOptions{
				LabelSelector: labelSelector.String(),
			},
		)
		if err != nil {
			return err
		}
		for _, snapshot := range snapshotList.Items {
			if _, _, err := util.PatchSnapshot(a.extClient.KubedbV1alpha1(), &snapshot, func(in *api.Snapshot) *api.Snapshot {
				in.ObjectMeta = core_util.RemoveOwnerReference(in.ObjectMeta, ref)
				return in
			}); err != nil {
				return err
			}
		}
	}

	// Remove Owner Reference of PVC
	pvcList, err := a.client.CoreV1().PersistentVolumeClaims(dormantDatabase.Namespace).List(
		metav1.ListOptions{
			LabelSelector: labelSelector.String(),
		},
	)
	if err != nil {
		return err
	}
	for _, pvc := range pvcList.Items {
		if _, _, err := core_util.PatchPVC(a.client, &pvc, func(in *coreV1.PersistentVolumeClaim) *coreV1.PersistentVolumeClaim {
			in.ObjectMeta = core_util.RemoveOwnerReference(in.ObjectMeta, ref)
			return in
		}); err != nil {
			return err
		}
	}

	// Remove owner reference from Secrets
	secretVolList := getDatabaseSecretName(dormantDatabase, dbKind)
	for _, secretVolSrc := range secretVolList {
		if secretVolSrc == nil {
			continue
		}

		secret, err := a.client.CoreV1().Secrets(dormantDatabase.Namespace).Get(secretVolSrc.SecretName, metav1.GetOptions{})
		if err != nil && kerr.IsNotFound(err) {
			continue
		} else if err != nil {
			return err
		}

		if _, _, err := core_util.PatchSecret(a.client, secret, func(in *coreV1.Secret) *coreV1.Secret {
			in.ObjectMeta = core_util.RemoveOwnerReference(in.ObjectMeta, ref)
			return in
		}); err != nil {
			return err
		}
	}

	// Remove owner reference from configMaps
	cfgList, err := a.client.CoreV1().ConfigMaps(dormantDatabase.Namespace).List(
		metav1.ListOptions{
			LabelSelector: labelSelector.String(),
		},
	)
	if err != nil {
		return err
	}
	for _, cfg := range cfgList.Items {
		if _, _, err := core_util.PatchConfigMap(a.client, &cfg, func(in *coreV1.ConfigMap) *coreV1.ConfigMap {
			in.ObjectMeta = core_util.RemoveOwnerReference(in.ObjectMeta, ref)
			return in
		}); err != nil {
			return err
		}
	}

	return nil
}

func getDatabaseSecretName(dormantDatabase *api.DormantDatabase, dbKind string) []*coreV1.SecretVolumeSource {
	if dbKind == api.ResourceKindMemcached || dbKind == api.ResourceKindRedis {
		return nil
	}
	switch dbKind {
	case api.ResourceKindMongoDB:
		secretVol := []*coreV1.SecretVolumeSource{
			dormantDatabase.Spec.Origin.Spec.MongoDB.DatabaseSecret,
		}
		if dormantDatabase.Spec.Origin.Spec.MongoDB.ClusterMode != nil &&
			dormantDatabase.Spec.Origin.Spec.MongoDB.ClusterMode.ReplicaSet != nil {
			secretVol = append(secretVol, dormantDatabase.Spec.Origin.Spec.MongoDB.ClusterMode.ReplicaSet.KeyFileSecret)
		}
		return secretVol
	case api.ResourceKindMySQL:
		return []*coreV1.SecretVolumeSource{dormantDatabase.Spec.Origin.Spec.MySQL.DatabaseSecret}
	case api.ResourceKindPostgres:
		return []*coreV1.SecretVolumeSource{dormantDatabase.Spec.Origin.Spec.Postgres.DatabaseSecret}
	case api.ResourceKindElasticsearch:
		return []*coreV1.SecretVolumeSource{
			dormantDatabase.Spec.Origin.Spec.Elasticsearch.DatabaseSecret,
			dormantDatabase.Spec.Origin.Spec.Elasticsearch.CertificateSecret,
		}
	}
	return nil
}

// SterilizeSecrets cleans secret that is created for this Ex-MongoDB (now DormantDatabase) database by KubeDB-Operator and
// not used by any other MongoDB or DormantDatabases objects.
func (a *DormantDatabaseValidator) sterilizeSecrets(dormantDatabase *api.DormantDatabase, secretVolume *coreV1.SecretVolumeSource) error {
	secretFound := false

	// Get object reference of dormant database
	ref, rerr := reference.GetReference(clientsetscheme.Scheme, dormantDatabase)
	if rerr != nil {
		return rerr
	}

	dbKind, err := meta_util.GetStringValue(dormantDatabase.ObjectMeta.Labels, api.LabelDatabaseKind)
	if err != nil {
		return err
	}

	if secretVolume == nil {
		return nil
	}

	secret, err := a.client.CoreV1().Secrets(dormantDatabase.Namespace).Get(secretVolume.SecretName, metav1.GetOptions{})
	if err != nil && kerr.IsNotFound(err) {
		return nil
	} else if err != nil {
		return err
	}

	// if api.LabelDatabaseKind not exists in secret, then the secret is not created by KubeDB-Operator
	// otherwise, probably KubeDB-Operator created the secrets.
	if _, err := meta_util.GetStringValue(secret.ObjectMeta.Labels, api.LabelDatabaseKind); err != nil {
		return nil
	}

	secretFound, err = a.isSecretUsedInExistingDB(dormantDatabase, dbKind, secretVolume)
	if err != nil {
		return err
	}

	if !secretFound {
		labelMap := map[string]string{
			api.LabelDatabaseKind: dbKind,
		}
		dormantDatabaseList, err := a.extClient.KubedbV1alpha1().DormantDatabases(dormantDatabase.Namespace).List(
			metav1.ListOptions{
				LabelSelector: labels.SelectorFromSet(labelMap).String(),
			},
		)
		if err != nil {
			return err
		}

		for _, ddb := range dormantDatabaseList.Items {
			if ddb.Name == dormantDatabase.Name {
				continue
			}

			databaseSecretList := getDatabaseSecretName(&ddb, dbKind)
			if databaseSecretList != nil {
				for _, databaseSecret := range databaseSecretList {
					if databaseSecret == nil {
						continue
					}
					if databaseSecret.SecretName == secretVolume.SecretName {
						secretFound = true
						break
					}
				}
			}
			if secretFound {
				break
			}
		}
	}

	if !secretFound {
		if _, _, err := core_util.PatchSecret(a.client, secret, func(in *coreV1.Secret) *coreV1.Secret {
			in.ObjectMeta = core_util.EnsureOwnerReference(in.ObjectMeta, ref)
			return in
		}); err != nil {
			return err
		}
	}

	return nil
}

func (a *DormantDatabaseValidator) isSecretUsedInExistingDB(dormantDatabase *api.DormantDatabase, dbKind string, secretVolume *coreV1.SecretVolumeSource) (bool, error) {
	if dbKind == api.ResourceKindMemcached || dbKind == api.ResourceKindRedis {
		return false, nil
	}
	switch dbKind {
	case api.ResourceKindMongoDB:
		mgList, err := a.extClient.KubedbV1alpha1().MongoDBs(dormantDatabase.Namespace).List(metav1.ListOptions{})
		if err != nil {
			return true, err
		}
		for _, mg := range mgList.Items {
			databaseSecret := mg.Spec.DatabaseSecret
			if databaseSecret != nil {
				if databaseSecret.SecretName == secretVolume.SecretName {
					return true, nil
				}
			}
		}
	case api.ResourceKindMySQL:
		msList, err := a.extClient.KubedbV1alpha1().MySQLs(dormantDatabase.Namespace).List(metav1.ListOptions{})
		if err != nil {
			return true, err
		}
		for _, ms := range msList.Items {
			databaseSecret := ms.Spec.DatabaseSecret
			if databaseSecret != nil {
				if databaseSecret.SecretName == secretVolume.SecretName {
					return true, nil
				}
			}
		}
	case api.ResourceKindPostgres:
		pgList, err := a.extClient.KubedbV1alpha1().Postgreses(dormantDatabase.Namespace).List(metav1.ListOptions{})
		if err != nil {
			return true, err
		}
		for _, pg := range pgList.Items {
			databaseSecret := pg.Spec.DatabaseSecret
			if databaseSecret != nil {
				if databaseSecret.SecretName == secretVolume.SecretName {
					return true, nil
				}
			}
		}
	case api.ResourceKindElasticsearch:
		esList, err := a.extClient.KubedbV1alpha1().Elasticsearches(dormantDatabase.Namespace).List(metav1.ListOptions{})
		if err != nil {
			return true, err
		}
		for _, es := range esList.Items {
			databaseSecret := es.Spec.DatabaseSecret
			if databaseSecret != nil {
				if databaseSecret.SecretName == secretVolume.SecretName {
					return true, nil
				}
			}
			certCertificate := es.Spec.CertificateSecret
			if certCertificate != nil {
				if certCertificate.SecretName == secretVolume.SecretName {
					return true, nil
				}
			}
		}
	}
	return false, nil
}
