package framework

import (
	"fmt"
	"time"

	"github.com/appscode/kutil/tools/portforward"
	"github.com/globalsign/mgo/bson"
	"github.com/go-bongo/bongo"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type KubedbTable struct {
	bongo.DocumentBase `bson:",inline"`
	FirstName          string
	LastName           string
}

func (f *Framework) GetMongoDBClient(meta metav1.ObjectMeta, dbName string, clientPodName string) (*bongo.Connection, error) {
	mongodb, err := f.GetMongoDB(meta)
	if err != nil {
		return nil, err
	}
	tunnel := portforward.NewTunnel(
		f.kubeClient.CoreV1().RESTClient(),
		f.restConfig,
		mongodb.Namespace,
		clientPodName,
		27017,
	)

	if err := tunnel.ForwardPort(); err != nil {
		return nil, err
	}
	user := "root"
	pass, err := f.GetMongoDBRootPassword(mongodb)

	fmt.Println(">>>>>>>>>>>>>>>>>>>>>>>>>>>", fmt.Sprintf("mongodb://%s:%s@127.0.0.1:%v", user, pass, tunnel.Local))

	config := &bongo.Config{
		ConnectionString: fmt.Sprintf("mongodb://%s:%s@127.0.0.1:%v", user, pass, tunnel.Local),
		Database:         dbName,
	}

	return bongo.Connect(config)

}

func (f *Framework) EventuallyInsertDocument(meta metav1.ObjectMeta, dbName string, clientPodName string) GomegaAsyncAssertion {
	return Eventually(
		func() bool {
			en, _ := f.GetMongoDBClient(meta, dbName, clientPodName)
			//if err != nil {
			//	fmt.Println("GetMongoDB Client error", err)
			//	return false
			//}
			if en == nil {
				fmt.Println(">>>>>>>>>>>>>>. bingo!! nil engine!!!")
				return false
			}
			fmt.Printf(">>>>>>>>> %v", en)

			defer en.Session.Close()

			if err := en.Session.Ping(); err != nil {
				fmt.Println("Ping error", err)
				return false
			}

			person := &KubedbTable{
				FirstName: "kubernetes",
				LastName:  "database",
			}

			if err := en.Collection("people").Save(person); err != nil {
				fmt.Println("creation error", err)
				return false
			}
			return true
		},
		time.Minute*15,
		time.Second*10,
	)
}

// fmt.Sprintf("%v-0", meta.Name)

func (f *Framework) EventuallyDocumentExists(meta metav1.ObjectMeta, dbName string, clientPodName string) GomegaAsyncAssertion {
	return Eventually(
		func() bool {
			en, err := f.GetMongoDBClient(meta, dbName, clientPodName)
			if err != nil {
				fmt.Println("GetMongoDB Client error", err)
				return false
			}
			defer en.Session.Close()

			if err := en.Session.Ping(); err != nil {
				fmt.Println("Ping error", err)
				return false
			}
			person := &KubedbTable{}

			if er := en.Collection("people").FindOne(bson.M{"firstname": "kubernetes"}, person); er == nil {
				return true
			} else {
				fmt.Println("checking error", er)
			}
			return false
		},
		time.Minute*15,
		time.Second*10,
	)
}

func (f *Framework) DocumentExistsInAllInstances(meta metav1.ObjectMeta, dbName string) error {
	mongodb, err := f.GetMongoDB(meta)
	if err != nil {
		return err
	}
	replica := int32(0)
	if mongodb.Spec.Replicas != nil {
		replica = *mongodb.Spec.Replicas
	}
	for i := int32(0); i < replica; i++ {
		clientPodName := fmt.Sprintf("%v-%v", meta.Name, i)
		By("Checking Inserted Document in RS " + clientPodName)
		f.EventuallyDocumentExists(mongodb.ObjectMeta, dbName, clientPodName).Should(BeTrue())
	}

	return nil
}
