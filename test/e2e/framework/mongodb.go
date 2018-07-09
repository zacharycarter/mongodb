package framework

import (
	"fmt"
	"time"

	"github.com/appscode/go/crypto/rand"
	jsonTypes "github.com/appscode/go/encoding/json/types"
	"github.com/appscode/go/types"
	api "github.com/kubedb/apimachinery/apis/kubedb/v1alpha1"
	"github.com/kubedb/apimachinery/client/clientset/versioned/typed/kubedb/v1alpha1/util"
	. "github.com/onsi/gomega"
	core "k8s.io/api/core/v1"
	kerr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (f *Invocation) MongoDB() *api.MongoDB {
	return &api.MongoDB{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rand.WithUniqSuffix("mongodb"),
			Namespace: f.namespace,
			Labels: map[string]string{
				"app": f.app,
			},
		},
		Spec: api.MongoDBSpec{
			Version: jsonTypes.StrYo(DBVersion),
			Storage: core.PersistentVolumeClaimSpec{
				Resources: core.ResourceRequirements{
					Requests: core.ResourceList{
						core.ResourceStorage: resource.MustParse("1Gi"),
					},
				},
				StorageClassName: types.StringP(f.StorageClass),
			},
		},
	}
}

func (f *Framework) CreateMongoDB(obj *api.MongoDB) error {
	_, err := f.extClient.MongoDBs(obj.Namespace).Create(obj)
	return err
}

func (f *Framework) GetMongoDB(meta metav1.ObjectMeta) (*api.MongoDB, error) {
	return f.extClient.MongoDBs(meta.Namespace).Get(meta.Name, metav1.GetOptions{})
}

func (f *Framework) PatchMongoDB(meta metav1.ObjectMeta, transform func(*api.MongoDB) *api.MongoDB) (*api.MongoDB, error) {
	mongodb, err := f.extClient.MongoDBs(meta.Namespace).Get(meta.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	mongodb, _, err = util.PatchMongoDB(f.extClient, mongodb, transform)
	return mongodb, err
}

func (f *Framework) DeleteMongoDB(meta metav1.ObjectMeta) error {
	return f.extClient.MongoDBs(meta.Namespace).Delete(meta.Name, deleteInBackground())
}

func (f *Framework) EventuallyMongoDB(meta metav1.ObjectMeta) GomegaAsyncAssertion {
	return Eventually(
		func() bool {
			_, err := f.extClient.MongoDBs(meta.Namespace).Get(meta.Name, metav1.GetOptions{})
			if err != nil {
				if kerr.IsNotFound(err) {
					return false
				}
				Expect(err).NotTo(HaveOccurred())
			}
			return true
		},
		time.Minute*10,
		time.Second*5,
	)
}

func (f *Framework) EventuallyMongoDBRunning(meta metav1.ObjectMeta) GomegaAsyncAssertion {
	return Eventually(
		func() bool {
			mongodb, err := f.extClient.MongoDBs(meta.Namespace).Get(meta.Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			return mongodb.Status.Phase == api.DatabasePhaseRunning
		},
		time.Minute*15,
		time.Second*5,
	)
}

func (f *Framework) CleanMongoDB() {
	mongodbList, err := f.extClient.MongoDBs(f.namespace).List(metav1.ListOptions{})
	if err != nil {
		return
	}
	for _, e := range mongodbList.Items {
		if _, _, err := util.PatchMongoDB(f.extClient, &e, func(in *api.MongoDB) *api.MongoDB {
			in.ObjectMeta.Finalizers = nil
			in.Spec.DoNotPause = false
			return in
		}); err != nil {
			fmt.Printf("error Patching MongoDB. error: %v", err)
		}
	}
	if err := f.extClient.MongoDBs(f.namespace).DeleteCollection(deleteInBackground(), metav1.ListOptions{}); err != nil {
		fmt.Printf("error in deletion of MongoDB. Error: %v", err)
	}
}
