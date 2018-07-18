package framework

import (
	"fmt"
	"time"

	"github.com/globalsign/mgo/bson"
	"github.com/go-bongo/bongo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Todo: use official go-mongodb driver. https://github.com/mongodb/mongo-go-driver
// Currently in Alpha Release.
//
// Connect to each replica set instances to check data.
// Currently `Secondary Nodes` not supported in used drivers.

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

	nodePortIP, err := f.GetNodePortIP(meta)
	if err != nil {
		return nil, err
	}

	user := "root"
	pass, err := f.GetMongoDBRootPassword(mongodb)

	fmt.Println(">>>>>>>>>>>>>>>>>>>>>>>>>>> 1", fmt.Sprintf("mongodb://%s:%s@%v", user, pass, nodePortIP))

	config := &bongo.Config{
		ConnectionString: fmt.Sprintf("mongodb://%s:%s@%v", user, pass, nodePortIP),
		Database:         dbName,
	}
	return bongo.Connect(config)
}

func (f *Framework) EventuallyInsertDocument(meta metav1.ObjectMeta, dbName string, clientPodName string) GomegaAsyncAssertion {
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
