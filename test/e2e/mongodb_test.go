package e2e_test

import (
	"fmt"
	"os"

	"github.com/appscode/go/types"
	meta_util "github.com/appscode/kutil/meta"
	api "github.com/kubedb/apimachinery/apis/kubedb/v1alpha1"
	"github.com/kubedb/apimachinery/client/clientset/versioned/typed/kubedb/v1alpha1/util"
	"github.com/kubedb/mongodb/test/e2e/framework"
	"github.com/kubedb/mongodb/test/e2e/matcher"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	core "k8s.io/api/core/v1"
	kerr "k8s.io/apimachinery/pkg/api/errors"
)

const (
	S3_BUCKET_NAME             = "S3_BUCKET_NAME"
	GCS_BUCKET_NAME            = "GCS_BUCKET_NAME"
	AZURE_CONTAINER_NAME       = "AZURE_CONTAINER_NAME"
	SWIFT_CONTAINER_NAME       = "SWIFT_CONTAINER_NAME"
	MONGO_INITDB_ROOT_USERNAME = "MONGO_INITDB_ROOT_USERNAME"
	MONGO_INITDB_ROOT_PASSWORD = "MONGO_INITDB_ROOT_PASSWORD"
	MONGO_INITDB_DATABASE      = "MONGO_INITDB_DATABASE"
)

var _ = Describe("MongoDB", func() {
	var (
		err                      error
		f                        *framework.Invocation
		mongodb                  *api.MongoDB
		garbageMongoDB           *api.MongoDBList
		snapshot                 *api.Snapshot
		secret                   *core.Secret
		skipMessage              string
		skipSnapshotDataChecking bool
		dbName                   string
	)

	BeforeEach(func() {
		f = root.Invoke()
		mongodb = f.MongoDBStandalone()
		garbageMongoDB = new(api.MongoDBList)
		snapshot = f.Snapshot()
		secret = new(core.Secret)
		skipMessage = ""
		skipSnapshotDataChecking = true
		dbName = "kubedb"
	})

	var createAndWaitForRunning = func() {
		By("Create MongoDB: " + mongodb.Name)
		err = f.CreateMongoDB(mongodb)
		Expect(err).NotTo(HaveOccurred())

		By("Wait for Running mongodb")
		f.EventuallyMongoDBRunning(mongodb.ObjectMeta).Should(BeTrue())
	}

	var deleteTestResource = func() {
		if mongodb == nil {
			Skip("Skipping")
		}
		By("Delete mongodb")
		err = f.DeleteMongoDB(mongodb.ObjectMeta)
		if err != nil {
			if kerr.IsNotFound(err) {
				// MongoDB was not created. Hence, rest of cleanup is not necessary.
				return
			}
			Expect(err).NotTo(HaveOccurred())
		}

		//By("Delete Test Service if exists")
		//err := f.DeleteTestService(mongodb.ObjectMeta)
		//Expect(err).NotTo(HaveOccurred())

		By("Wait for mongodb to be paused")
		f.EventuallyDormantDatabaseStatus(mongodb.ObjectMeta).Should(matcher.HavePaused())

		By("Set DormantDatabase Spec.WipeOut to true")
		_, err = f.PatchDormantDatabase(mongodb.ObjectMeta, func(in *api.DormantDatabase) *api.DormantDatabase {
			in.Spec.WipeOut = true
			return in
		})
		Expect(err).NotTo(HaveOccurred())

		By("Delete Dormant Database")
		err = f.DeleteDormantDatabase(mongodb.ObjectMeta)
		if !kerr.IsNotFound(err) {
			Expect(err).NotTo(HaveOccurred())
		}

		By("Wait for mongodb resources to be wipedOut")
		f.EventuallyWipedOut(mongodb.ObjectMeta).Should(Succeed())
	}

	AfterEach(func() {
		// Delete test resource
		deleteTestResource()

		for _, mg := range garbageMongoDB.Items {
			*mongodb = mg
			// Delete test resource
			deleteTestResource()
		}

		if !skipSnapshotDataChecking {
			By("Check for snapshot data")
			f.EventuallySnapshotDataFound(snapshot).Should(BeFalse())
		}

		if secret != nil {
			f.DeleteSecret(secret.ObjectMeta)
		}
	})

	Describe("Test", func() {
		BeforeEach(func() {
			if f.StorageClass == "" {
				Skip("Missing StorageClassName. Provide as flag to test this.")
			}
		})

		Context("General", func() {

			Context("With PVC", func() {

				var shouldRunWithPVC = func() {
					if skipMessage != "" {
						Skip(skipMessage)
					}
					// Create MySQL
					createAndWaitForRunning()

					By("Insert Document Inside DB")
					f.EventuallyInsertDocument(mongodb.ObjectMeta, dbName).Should(BeTrue())

					By("Checking Inserted Document")
					f.EventuallyDocumentExists(mongodb.ObjectMeta, dbName).Should(BeTrue())

					By("Delete mongodb")
					err = f.DeleteMongoDB(mongodb.ObjectMeta)
					Expect(err).NotTo(HaveOccurred())

					By("Wait for mongodb to be paused")
					f.EventuallyDormantDatabaseStatus(mongodb.ObjectMeta).Should(matcher.HavePaused())

					// Create MongoDB object again to resume it
					By("Create MongoDB: " + mongodb.Name)
					err = f.CreateMongoDB(mongodb)
					Expect(err).NotTo(HaveOccurred())

					By("Wait for DormantDatabase to be deleted")
					f.EventuallyDormantDatabase(mongodb.ObjectMeta).Should(BeFalse())

					By("Wait for Running mongodb")
					f.EventuallyMongoDBRunning(mongodb.ObjectMeta).Should(BeTrue())

					By("Checking Inserted Document")
					f.EventuallyDocumentExists(mongodb.ObjectMeta, dbName).Should(BeTrue())
				}

				It("should run successfully", shouldRunWithPVC)

				Context("With Replica Set", func() {
					BeforeEach(func() {
						mongodb = f.MongoDBRS()
						mongodb.Spec.Replicas = types.Int32P(3)
					})
					It("should run successfully", shouldRunWithPVC)
				})

			})
		})

		Context("DoNotPause", func() {
			BeforeEach(func() {
				mongodb.Spec.DoNotPause = true
			})

			var shouldWorkDoNotPause = func() {
				// Create and wait for running MongoDB
				createAndWaitForRunning()

				By("Delete mongodb")
				err = f.DeleteMongoDB(mongodb.ObjectMeta)
				Expect(err).Should(HaveOccurred())

				By("MongoDB is not paused. Check for mongodb")
				f.EventuallyMongoDB(mongodb.ObjectMeta).Should(BeTrue())

				By("Check for Running mongodb")
				f.EventuallyMongoDBRunning(mongodb.ObjectMeta).Should(BeTrue())

				By("Update mongodb to set DoNotPause=false")
				f.PatchMongoDB(mongodb.ObjectMeta, func(in *api.MongoDB) *api.MongoDB {
					in.Spec.DoNotPause = false
					return in
				})
			}
			It("should work successfully", shouldWorkDoNotPause)

			Context("With Replica Set", func() {
				BeforeEach(func() {
					mongodb = f.MongoDBRS()
					mongodb.Spec.DoNotPause = true
				})
				It("should run successfully", shouldWorkDoNotPause)
			})

		})

		Context("Snapshot", func() {
			BeforeEach(func() {
				skipSnapshotDataChecking = false
				snapshot.Spec.DatabaseName = mongodb.Name
			})

			var shouldTakeSnapshot = func() {
				// Create and wait for running MongoDB
				createAndWaitForRunning()

				By("Create Secret")
				err := f.CreateSecret(secret)
				Expect(err).NotTo(HaveOccurred())

				By("Create Snapshot")
				err = f.CreateSnapshot(snapshot)
				Expect(err).NotTo(HaveOccurred())

				By("Check for Succeeded snapshot")
				f.EventuallySnapshotPhase(snapshot.ObjectMeta).Should(Equal(api.SnapshotPhaseSucceeded))

				if !skipSnapshotDataChecking {
					By("Check for snapshot data")
					f.EventuallySnapshotDataFound(snapshot).Should(BeTrue())
				}
			}

			Context("In Local", func() {
				BeforeEach(func() {
					skipSnapshotDataChecking = true
					secret = f.SecretForLocalBackend()
					snapshot.Spec.StorageSecretName = secret.Name
					snapshot.Spec.Local = &api.LocalSpec{
						MountPath: "/repo",
						VolumeSource: core.VolumeSource{
							EmptyDir: &core.EmptyDirVolumeSource{},
						},
					}
				})

				It("should take Snapshot successfully", shouldTakeSnapshot)

				Context("With Replica Set", func() {
					BeforeEach(func() {
						mongodb = f.MongoDBRS()
						snapshot.Spec.DatabaseName = mongodb.Name
					})
					It("should take Snapshot successfully", shouldTakeSnapshot)
				})
			})

			Context("In S3", func() {
				BeforeEach(func() {
					secret = f.SecretForS3Backend()
					snapshot.Spec.StorageSecretName = secret.Name
					snapshot.Spec.S3 = &api.S3Spec{
						Bucket: os.Getenv(S3_BUCKET_NAME),
					}
				})

				It("should take Snapshot successfully", shouldTakeSnapshot)
			})

			Context("In GCS", func() {
				BeforeEach(func() {
					secret = f.SecretForGCSBackend()
					snapshot.Spec.StorageSecretName = secret.Name
					snapshot.Spec.GCS = &api.GCSSpec{
						Bucket: os.Getenv(GCS_BUCKET_NAME),
					}
				})

				It("should take Snapshot successfully", shouldTakeSnapshot)

				Context("With Replica Set", func() {
					BeforeEach(func() {
						mongodb = f.MongoDBRS()
						mongodb.Spec.Replicas = types.Int32P(3)
						snapshot.Spec.DatabaseName = mongodb.Name
					})
					It("should take Snapshot successfully", shouldTakeSnapshot)
				})

				Context("Delete One Snapshot keeping others", func() {
					BeforeEach(func() {
						mongodb.Spec.Init = &api.InitSpec{
							ScriptSource: &api.ScriptSourceSpec{
								VolumeSource: core.VolumeSource{
									GitRepo: &core.GitRepoVolumeSource{
										Repository: "https://github.com/kubedb/mongodb-init-scripts.git",
										Directory:  ".",
									},
								},
							},
						}
					})

					It("Delete One Snapshot keeping others", func() {
						// Create and wait for running MongoDB
						createAndWaitForRunning()

						By("Create Secret")
						err := f.CreateSecret(secret)
						Expect(err).NotTo(HaveOccurred())

						By("Create Snapshot")
						err = f.CreateSnapshot(snapshot)
						Expect(err).NotTo(HaveOccurred())

						By("Check for Succeeded snapshot")
						f.EventuallySnapshotPhase(snapshot.ObjectMeta).Should(Equal(api.SnapshotPhaseSucceeded))

						if !skipSnapshotDataChecking {
							By("Check for snapshot data")
							f.EventuallySnapshotDataFound(snapshot).Should(BeTrue())
						}

						oldSnapshot := snapshot

						// create new Snapshot
						snapshot := f.Snapshot()
						snapshot.Spec.DatabaseName = mongodb.Name
						snapshot.Spec.StorageSecretName = secret.Name
						snapshot.Spec.GCS = &api.GCSSpec{
							Bucket: os.Getenv(GCS_BUCKET_NAME),
						}

						By("Create Snapshot")
						err = f.CreateSnapshot(snapshot)
						Expect(err).NotTo(HaveOccurred())

						By("Check for Succeeded snapshot")
						f.EventuallySnapshotPhase(snapshot.ObjectMeta).Should(Equal(api.SnapshotPhaseSucceeded))

						if !skipSnapshotDataChecking {
							By("Check for snapshot data")
							f.EventuallySnapshotDataFound(snapshot).Should(BeTrue())
						}

						By(fmt.Sprintf("Delete Snapshot %v", snapshot.Name))
						err = f.DeleteSnapshot(snapshot.ObjectMeta)
						Expect(err).NotTo(HaveOccurred())

						By("Wait for Deleting Snapshot")
						f.EventuallySnapshot(mongodb.ObjectMeta).Should(BeFalse())
						if !skipSnapshotDataChecking {
							By("Check for snapshot data")
							f.EventuallySnapshotDataFound(snapshot).Should(BeFalse())
						}

						snapshot = oldSnapshot

						By(fmt.Sprintf("Old Snapshot %v Still Exists", snapshot.Name))
						_, err = f.GetSnapshot(snapshot.ObjectMeta)
						Expect(err).NotTo(HaveOccurred())

						if !skipSnapshotDataChecking {
							By(fmt.Sprintf("Check for old snapshot %v data", snapshot.Name))
							f.EventuallySnapshotDataFound(snapshot).Should(BeTrue())
						}
					})
				})
			})

			Context("In Azure", func() {
				BeforeEach(func() {
					secret = f.SecretForAzureBackend()
					snapshot.Spec.StorageSecretName = secret.Name
					snapshot.Spec.Azure = &api.AzureSpec{
						Container: os.Getenv(AZURE_CONTAINER_NAME),
					}
				})

				It("should take Snapshot successfully", shouldTakeSnapshot)
			})

			Context("In Swift", func() {
				BeforeEach(func() {
					secret = f.SecretForSwiftBackend()
					snapshot.Spec.StorageSecretName = secret.Name
					snapshot.Spec.Swift = &api.SwiftSpec{
						Container: os.Getenv(SWIFT_CONTAINER_NAME),
					}
				})

				It("should take Snapshot successfully", shouldTakeSnapshot)
			})
		})

		Context("Initialize", func() {

			Context("With Script", func() {
				BeforeEach(func() {
					mongodb.Spec.Init = &api.InitSpec{
						ScriptSource: &api.ScriptSourceSpec{
							VolumeSource: core.VolumeSource{
								GitRepo: &core.GitRepoVolumeSource{
									Repository: "https://github.com/kubedb/mongodb-init-scripts.git",
									Directory:  ".",
								},
							},
						},
					}
				})

				It("should run successfully", func() {
					// Create MongoDB
					createAndWaitForRunning()

					By("Checking Inserted Document")
					f.EventuallyDocumentExists(mongodb.ObjectMeta, dbName).Should(BeTrue())
				})

				Context("With Replica Set", func() {
					BeforeEach(func() {
						mongodb = f.MongoDBRS()
						mongodb.Spec.Replicas = types.Int32P(3)
						mongodb.Spec.Init = &api.InitSpec{
							ScriptSource: &api.ScriptSourceSpec{
								VolumeSource: core.VolumeSource{
									GitRepo: &core.GitRepoVolumeSource{
										Repository: "https://github.com/kubedb/mongodb-init-scripts.git",
										Directory:  ".",
									},
								},
							},
						}
					})
					It("should Initialize successfully", func() {
						// Create MongoDB
						createAndWaitForRunning()

						By("Checking Inserted Document")
						f.EventuallyDocumentExists(mongodb.ObjectMeta, dbName).Should(BeTrue())
					})
				})

			})

			Context("With Snapshot", func() {

				var anotherMongoDB *api.MongoDB

				BeforeEach(func() {
					anotherMongoDB = f.MongoDBStandalone()
					skipSnapshotDataChecking = false
					secret = f.SecretForGCSBackend()
					snapshot.Spec.StorageSecretName = secret.Name
					snapshot.Spec.GCS = &api.GCSSpec{
						Bucket: os.Getenv(GCS_BUCKET_NAME),
					}
					snapshot.Spec.DatabaseName = mongodb.Name
				})

				var shouldInitializeSnapshot = func() {
					// Create and wait for running MongoDB
					createAndWaitForRunning()

					By("Insert Document Inside DB")
					f.EventuallyInsertDocument(mongodb.ObjectMeta, dbName).Should(BeTrue())

					By("Checking Inserted Document")
					f.EventuallyDocumentExists(mongodb.ObjectMeta, dbName).Should(BeTrue())

					By("Create Secret")
					f.CreateSecret(secret)

					By("Create Snapshot")
					f.CreateSnapshot(snapshot)

					By("Check for Succeeded snapshot")
					f.EventuallySnapshotPhase(snapshot.ObjectMeta).Should(Equal(api.SnapshotPhaseSucceeded))

					By("Check for snapshot data")
					f.EventuallySnapshotDataFound(snapshot).Should(BeTrue())

					oldMongoDB, err := f.GetMongoDB(mongodb.ObjectMeta)
					Expect(err).NotTo(HaveOccurred())

					garbageMongoDB.Items = append(garbageMongoDB.Items, *oldMongoDB)

					By("Create mongodb from snapshot")
					mongodb = f.MongoDBStandalone()
					mongodb.Spec.Init = &api.InitSpec{
						SnapshotSource: &api.SnapshotSourceSpec{
							Namespace: snapshot.Namespace,
							Name:      snapshot.Name,
						},
					}

					// Create and wait for running MongoDB
					createAndWaitForRunning()

					By("Checking Inserted Document")
					f.EventuallyDocumentExists(mongodb.ObjectMeta, dbName).Should(BeTrue())
				}

				It("should run successfully", shouldInitializeSnapshot)

				Context("With Replica Set", func() {
					BeforeEach(func() {
						mongodb = f.MongoDBRS()
						snapshot.Spec.DatabaseName = mongodb.Name
						anotherMongoDB = f.MongoDBRS()
					})
					It("should take Snapshot successfully", shouldInitializeSnapshot)
				})
			})
		})

		Context("Resume", func() {
			var usedInitScript bool
			var usedInitSnapshot bool
			BeforeEach(func() {
				usedInitScript = false
				usedInitSnapshot = false
			})

			Context("Super Fast User - Create-Delete-Create-Delete-Create ", func() {
				It("should resume DormantDatabase successfully", func() {
					// Create and wait for running MongoDB
					createAndWaitForRunning()

					By("Insert Document Inside DB")
					f.EventuallyInsertDocument(mongodb.ObjectMeta, dbName).Should(BeTrue())

					By("Checking Inserted Document")
					f.EventuallyDocumentExists(mongodb.ObjectMeta, dbName).Should(BeTrue())

					By("Delete mongodb")
					err = f.DeleteMongoDB(mongodb.ObjectMeta)
					Expect(err).NotTo(HaveOccurred())

					By("Wait for mongodb to be paused")
					f.EventuallyDormantDatabaseStatus(mongodb.ObjectMeta).Should(matcher.HavePaused())

					// Create MongoDB object again to resume it
					By("Create MongoDB: " + mongodb.Name)
					err = f.CreateMongoDB(mongodb)
					Expect(err).NotTo(HaveOccurred())

					// Delete without caring if DB is resumed
					By("Delete mongodb")
					err = f.DeleteMongoDB(mongodb.ObjectMeta)
					Expect(err).NotTo(HaveOccurred())

					By("Wait for MongoDB to be deleted")
					f.EventuallyMongoDB(mongodb.ObjectMeta).Should(BeFalse())

					// Create MongoDB object again to resume it
					By("Create MongoDB: " + mongodb.Name)
					err = f.CreateMongoDB(mongodb)
					Expect(err).NotTo(HaveOccurred())

					By("Wait for DormantDatabase to be deleted")
					f.EventuallyDormantDatabase(mongodb.ObjectMeta).Should(BeFalse())

					By("Wait for Running mongodb")
					f.EventuallyMongoDBRunning(mongodb.ObjectMeta).Should(BeTrue())

					By("Checking Inserted Document")
					f.EventuallyDocumentExists(mongodb.ObjectMeta, dbName).Should(BeTrue())

					_, err = f.GetMongoDB(mongodb.ObjectMeta)
					Expect(err).NotTo(HaveOccurred())
				})
			})

			Context("Without Init", func() {

				var shouldResumeWithoutInit = func() {
					// Create and wait for running MongoDB
					createAndWaitForRunning()

					By("Insert Document Inside DB")
					f.EventuallyInsertDocument(mongodb.ObjectMeta, dbName).Should(BeTrue())

					By("Checking Inserted Document")
					f.EventuallyDocumentExists(mongodb.ObjectMeta, dbName).Should(BeTrue())

					By("Delete mongodb")
					err = f.DeleteMongoDB(mongodb.ObjectMeta)
					Expect(err).NotTo(HaveOccurred())

					By("Wait for mongodb to be paused")
					f.EventuallyDormantDatabaseStatus(mongodb.ObjectMeta).Should(matcher.HavePaused())

					// Create MongoDB object again to resume it
					By("Create MongoDB: " + mongodb.Name)
					err = f.CreateMongoDB(mongodb)
					Expect(err).NotTo(HaveOccurred())

					By("Wait for DormantDatabase to be deleted")
					f.EventuallyDormantDatabase(mongodb.ObjectMeta).Should(BeFalse())

					By("Wait for Running mongodb")
					f.EventuallyMongoDBRunning(mongodb.ObjectMeta).Should(BeTrue())

					By("Checking Inserted Document")
					f.EventuallyDocumentExists(mongodb.ObjectMeta, dbName).Should(BeTrue())

					_, err = f.GetMongoDB(mongodb.ObjectMeta)
					Expect(err).NotTo(HaveOccurred())
				}

				It("should resume DormantDatabase successfully", shouldResumeWithoutInit)

				Context("With Replica Set", func() {
					BeforeEach(func() {
						mongodb = f.MongoDBRS()
					})
					It("should take Snapshot successfully", shouldResumeWithoutInit)
				})
			})

			Context("with init Script", func() {
				BeforeEach(func() {
					usedInitScript = true
					mongodb.Spec.Init = &api.InitSpec{
						ScriptSource: &api.ScriptSourceSpec{
							VolumeSource: core.VolumeSource{
								GitRepo: &core.GitRepoVolumeSource{
									Repository: "https://github.com/kubedb/mongodb-init-scripts.git",
									Directory:  ".",
								},
							},
						},
					}
				})

				var shouldResumeWithInit = func() {
					// Create and wait for running MongoDB
					createAndWaitForRunning()

					By("Checking Inserted Document")
					f.EventuallyDocumentExists(mongodb.ObjectMeta, dbName).Should(BeTrue())

					By("Delete mongodb")
					err = f.DeleteMongoDB(mongodb.ObjectMeta)
					Expect(err).NotTo(HaveOccurred())

					By("Wait for mongodb to be paused")
					f.EventuallyDormantDatabaseStatus(mongodb.ObjectMeta).Should(matcher.HavePaused())

					// Create MongoDB object again to resume it
					By("Create MongoDB: " + mongodb.Name)
					err = f.CreateMongoDB(mongodb)
					Expect(err).NotTo(HaveOccurred())

					By("Wait for DormantDatabase to be deleted")
					f.EventuallyDormantDatabase(mongodb.ObjectMeta).Should(BeFalse())

					By("Wait for Running mongodb")
					f.EventuallyMongoDBRunning(mongodb.ObjectMeta).Should(BeTrue())

					By("Checking Inserted Document")
					f.EventuallyDocumentExists(mongodb.ObjectMeta, dbName).Should(BeTrue())

					mg, err := f.GetMongoDB(mongodb.ObjectMeta)
					Expect(err).NotTo(HaveOccurred())

					*mongodb = *mg
					if usedInitScript {
						Expect(mongodb.Spec.Init).ShouldNot(BeNil())
						_, err := meta_util.GetString(mongodb.Annotations, api.AnnotationInitialized)
						Expect(err).To(HaveOccurred())
					}
				}

				It("should resume DormantDatabase successfully", shouldResumeWithInit)

				Context("With Replica Set", func() {
					BeforeEach(func() {
						mongodb = f.MongoDBRS()
						mongodb.Spec.Init = &api.InitSpec{
							ScriptSource: &api.ScriptSourceSpec{
								VolumeSource: core.VolumeSource{
									GitRepo: &core.GitRepoVolumeSource{
										Repository: "https://github.com/kubedb/mongodb-init-scripts.git",
										Directory:  ".",
									},
								},
							},
						}
					})
					It("should take Snapshot successfully", shouldResumeWithInit)
				})

			})

			Context("With Snapshot Init", func() {

				var anotherMongoDB *api.MongoDB

				BeforeEach(func() {
					anotherMongoDB = f.MongoDBStandalone()
					usedInitSnapshot = true
					secret = f.SecretForGCSBackend()
					snapshot.Spec.StorageSecretName = secret.Name
					snapshot.Spec.GCS = &api.GCSSpec{
						Bucket: os.Getenv(GCS_BUCKET_NAME),
					}
					snapshot.Spec.DatabaseName = mongodb.Name
				})
				var shouldResumeWithSnapshot = func() {
					// Create and wait for running MongoDB
					createAndWaitForRunning()

					By("Insert Document Inside DB")
					f.EventuallyInsertDocument(mongodb.ObjectMeta, dbName).Should(BeTrue())

					By("Checking Inserted Document")
					f.EventuallyDocumentExists(mongodb.ObjectMeta, dbName).Should(BeTrue())

					By("Create Secret")
					f.CreateSecret(secret)

					By("Create Snapshot")
					f.CreateSnapshot(snapshot)

					By("Check for Succeeded snapshot")
					f.EventuallySnapshotPhase(snapshot.ObjectMeta).Should(Equal(api.SnapshotPhaseSucceeded))

					By("Check for snapshot data")
					f.EventuallySnapshotDataFound(snapshot).Should(BeTrue())

					oldMongoDB, err := f.GetMongoDB(mongodb.ObjectMeta)
					Expect(err).NotTo(HaveOccurred())

					garbageMongoDB.Items = append(garbageMongoDB.Items, *oldMongoDB)

					By("Create mongodb from snapshot")
					mongodb = anotherMongoDB
					mongodb.Spec.Init = &api.InitSpec{
						SnapshotSource: &api.SnapshotSourceSpec{
							Namespace: snapshot.Namespace,
							Name:      snapshot.Name,
						},
					}

					// Create and wait for running MongoDB
					createAndWaitForRunning()

					By("Checking Inserted Document")
					f.EventuallyDocumentExists(mongodb.ObjectMeta, dbName).Should(BeTrue())

					By("Delete mongodb")
					err = f.DeleteMongoDB(mongodb.ObjectMeta)
					Expect(err).NotTo(HaveOccurred())

					By("Wait for mongodb to be paused")
					f.EventuallyDormantDatabaseStatus(mongodb.ObjectMeta).Should(matcher.HavePaused())

					// Create MongoDB object again to resume it
					By("Create MongoDB: " + mongodb.Name)
					err = f.CreateMongoDB(mongodb)
					Expect(err).NotTo(HaveOccurred())

					By("Wait for DormantDatabase to be deleted")
					f.EventuallyDormantDatabase(mongodb.ObjectMeta).Should(BeFalse())

					By("Wait for Running mongodb")
					f.EventuallyMongoDBRunning(mongodb.ObjectMeta).Should(BeTrue())

					mongodb, err = f.GetMongoDB(mongodb.ObjectMeta)
					Expect(err).NotTo(HaveOccurred())

					By("Checking Inserted Document")
					f.EventuallyDocumentExists(mongodb.ObjectMeta, dbName).Should(BeTrue())

					if usedInitSnapshot {
						_, err = meta_util.GetString(mongodb.Annotations, api.AnnotationInitialized)
						Expect(err).NotTo(HaveOccurred())
					}
				}

				It("should resume successfully", shouldResumeWithSnapshot)

				Context("With Replica Set", func() {
					BeforeEach(func() {
						mongodb = f.MongoDBRS()
						snapshot.Spec.DatabaseName = mongodb.Name
						anotherMongoDB = f.MongoDBRS()
					})
					It("should take Snapshot successfully", shouldResumeWithSnapshot)
				})
			})

			Context("Multiple times with init script", func() {
				BeforeEach(func() {
					usedInitScript = true
					mongodb.Spec.Init = &api.InitSpec{
						ScriptSource: &api.ScriptSourceSpec{
							VolumeSource: core.VolumeSource{
								GitRepo: &core.GitRepoVolumeSource{
									Repository: "https://github.com/kubedb/mongodb-init-scripts.git",
									Directory:  ".",
								},
							},
						},
					}
				})

				var shouldResumeMultipleTimes = func() {
					// Create and wait for running MongoDB
					createAndWaitForRunning()

					By("Checking Inserted Document")
					f.EventuallyDocumentExists(mongodb.ObjectMeta, dbName).Should(BeTrue())

					for i := 0; i < 3; i++ {
						By(fmt.Sprintf("%v-th", i+1) + " time running.")
						By("Delete mongodb")
						err = f.DeleteMongoDB(mongodb.ObjectMeta)
						Expect(err).NotTo(HaveOccurred())

						By("Wait for mongodb to be paused")
						f.EventuallyDormantDatabaseStatus(mongodb.ObjectMeta).Should(matcher.HavePaused())

						// Create MongoDB object again to resume it
						By("Create MongoDB: " + mongodb.Name)
						err = f.CreateMongoDB(mongodb)
						Expect(err).NotTo(HaveOccurred())

						By("Wait for DormantDatabase to be deleted")
						f.EventuallyDormantDatabase(mongodb.ObjectMeta).Should(BeFalse())

						By("Wait for Running mongodb")
						f.EventuallyMongoDBRunning(mongodb.ObjectMeta).Should(BeTrue())

						_, err := f.GetMongoDB(mongodb.ObjectMeta)
						Expect(err).NotTo(HaveOccurred())

						By("Checking Inserted Document")
						f.EventuallyDocumentExists(mongodb.ObjectMeta, dbName).Should(BeTrue())

						if usedInitScript {
							Expect(mongodb.Spec.Init).ShouldNot(BeNil())
							_, err := meta_util.GetString(mongodb.Annotations, api.AnnotationInitialized)
							Expect(err).To(HaveOccurred())
						}
					}
				}

				It("should resume DormantDatabase successfully", shouldResumeMultipleTimes)

				Context("With Replica Set", func() {
					BeforeEach(func() {
						mongodb = f.MongoDBRS()
						mongodb.Spec.Init = &api.InitSpec{
							ScriptSource: &api.ScriptSourceSpec{
								VolumeSource: core.VolumeSource{
									GitRepo: &core.GitRepoVolumeSource{
										Repository: "https://github.com/kubedb/mongodb-init-scripts.git",
										Directory:  ".",
									},
								},
							},
						}
					})
					It("should take Snapshot successfully", shouldResumeMultipleTimes)
				})
			})

		})

		Context("SnapshotScheduler", func() {

			Context("With Startup", func() {

				var shouldStartupSchedular = func() {
					By("Create Secret")
					f.CreateSecret(secret)

					// Create and wait for running MongoDB
					createAndWaitForRunning()

					By("Count multiple Snapshot Object")
					f.EventuallySnapshotCount(mongodb.ObjectMeta).Should(matcher.MoreThan(3))

					By("Remove Backup Scheduler from MongoDB")
					_, err = f.PatchMongoDB(mongodb.ObjectMeta, func(in *api.MongoDB) *api.MongoDB {
						in.Spec.BackupSchedule = nil
						return in
					})
					Expect(err).NotTo(HaveOccurred())

					By("Verify multiple Succeeded Snapshot")
					f.EventuallyMultipleSnapshotFinishedProcessing(mongodb.ObjectMeta).Should(Succeed())
				}

				Context("with local", func() {
					BeforeEach(func() {
						secret = f.SecretForLocalBackend()
						mongodb.Spec.BackupSchedule = &api.BackupScheduleSpec{
							CronExpression: "@every 20s",
							SnapshotStorageSpec: api.SnapshotStorageSpec{
								StorageSecretName: secret.Name,
								Local: &api.LocalSpec{
									MountPath: "/repo",
									VolumeSource: core.VolumeSource{
										EmptyDir: &core.EmptyDirVolumeSource{},
									},
								},
							},
						}
					})

					It("should run schedular successfully", shouldStartupSchedular)
				})

				Context("with GCS", func() {
					BeforeEach(func() {
						secret = f.SecretForGCSBackend()
						mongodb.Spec.BackupSchedule = &api.BackupScheduleSpec{
							CronExpression: "@every 20s",
							SnapshotStorageSpec: api.SnapshotStorageSpec{
								StorageSecretName: secret.Name,
								GCS: &api.GCSSpec{
									Bucket: os.Getenv(GCS_BUCKET_NAME),
								},
							},
						}
					})

					It("should run schedular successfully", shouldStartupSchedular)

					Context("With Replica Set", func() {
						BeforeEach(func() {
							mongodb = f.MongoDBRS()
							mongodb.Spec.BackupSchedule = &api.BackupScheduleSpec{
								CronExpression: "@every 20s",
								SnapshotStorageSpec: api.SnapshotStorageSpec{
									StorageSecretName: secret.Name,
									Local: &api.LocalSpec{
										MountPath: "/repo",
										VolumeSource: core.VolumeSource{
											EmptyDir: &core.EmptyDirVolumeSource{},
										},
									},
								},
							}
						})
						It("should take Snapshot successfully", shouldStartupSchedular)
					})
				})
			})

			Context("With Update - with Local", func() {
				BeforeEach(func() {
					secret = f.SecretForLocalBackend()
				})

				var shouldScheduleWithUpdate = func() {
					// Create and wait for running MongoDB
					createAndWaitForRunning()

					By("Create Secret")
					f.CreateSecret(secret)

					By("Update mongodb")
					_, err = f.PatchMongoDB(mongodb.ObjectMeta, func(in *api.MongoDB) *api.MongoDB {
						in.Spec.BackupSchedule = &api.BackupScheduleSpec{
							CronExpression: "@every 20s",
							SnapshotStorageSpec: api.SnapshotStorageSpec{
								StorageSecretName: secret.Name,
								Local: &api.LocalSpec{
									MountPath: "/repo",
									VolumeSource: core.VolumeSource{
										EmptyDir: &core.EmptyDirVolumeSource{},
									},
								},
							},
						}

						return in
					})
					Expect(err).NotTo(HaveOccurred())

					By("Count multiple Snapshot Object")
					f.EventuallySnapshotCount(mongodb.ObjectMeta).Should(matcher.MoreThan(3))

					By("Remove Backup Scheduler from MongoDB")
					_, err = f.PatchMongoDB(mongodb.ObjectMeta, func(in *api.MongoDB) *api.MongoDB {
						in.Spec.BackupSchedule = nil
						return in
					})
					Expect(err).NotTo(HaveOccurred())

					By("Verify multiple Succeeded Snapshot")
					f.EventuallyMultipleSnapshotFinishedProcessing(mongodb.ObjectMeta).Should(Succeed())

					deleteTestResource()
				}

				It("should run schedular successfully", shouldScheduleWithUpdate)

				Context("With Replica Set", func() {
					BeforeEach(func() {
						mongodb = f.MongoDBRS()
					})
					It("should take Snapshot successfully", shouldScheduleWithUpdate)
				})
			})

			FContext("Re-Use DormantDatabase's scheduler", func() {
				BeforeEach(func() {
					secret = f.SecretForLocalBackend()
				})

				var shouldeReUseDormantDBcheduler = func() {
					// Create and wait for running MongoDB
					createAndWaitForRunning()

					By("Create Secret")
					f.CreateSecret(secret)

					By("Update mongodb")
					_, err = f.PatchMongoDB(mongodb.ObjectMeta, func(in *api.MongoDB) *api.MongoDB {
						in.Spec.BackupSchedule = &api.BackupScheduleSpec{
							CronExpression: "@every 20s",
							SnapshotStorageSpec: api.SnapshotStorageSpec{
								StorageSecretName: secret.Name,
								Local: &api.LocalSpec{
									MountPath: "/repo",
									VolumeSource: core.VolumeSource{
										EmptyDir: &core.EmptyDirVolumeSource{},
									},
								},
							},
						}
						return in
					})
					Expect(err).NotTo(HaveOccurred())

					By("Insert Document Inside DB")
					f.EventuallyInsertDocument(mongodb.ObjectMeta, dbName).Should(BeTrue())

					By("Checking Inserted Document")
					f.EventuallyDocumentExists(mongodb.ObjectMeta, dbName).Should(BeTrue())

					By("Count multiple Snapshot Object")
					f.EventuallySnapshotCount(mongodb.ObjectMeta).Should(matcher.MoreThan(3))

					By("Delete mongodb")
					err = f.DeleteMongoDB(mongodb.ObjectMeta)
					Expect(err).NotTo(HaveOccurred())

					By("Wait for mongodb to be paused")
					f.EventuallyDormantDatabaseStatus(mongodb.ObjectMeta).Should(matcher.HavePaused())

					// Create MongoDB object again to resume it
					By("Create MongoDB: " + mongodb.Name)
					err = f.CreateMongoDB(mongodb)
					Expect(err).NotTo(HaveOccurred())

					By("Wait for DormantDatabase to be deleted")
					f.EventuallyDormantDatabase(mongodb.ObjectMeta).Should(BeFalse())

					By("Wait for Running mongodb")
					f.EventuallyMongoDBRunning(mongodb.ObjectMeta).Should(BeTrue())

					By("Checking Inserted Document")
					f.EventuallyDocumentExists(mongodb.ObjectMeta, dbName).Should(BeTrue())

					By("Count multiple Snapshot Object")
					f.EventuallySnapshotCount(mongodb.ObjectMeta).Should(matcher.MoreThan(5))

					By("Remove Backup Scheduler from MongoDB")
					_, err = f.PatchMongoDB(mongodb.ObjectMeta, func(in *api.MongoDB) *api.MongoDB {
						in.Spec.BackupSchedule = nil
						return in
					})
					Expect(err).NotTo(HaveOccurred())

					By("Verify multiple Succeeded Snapshot")
					f.EventuallyMultipleSnapshotFinishedProcessing(mongodb.ObjectMeta).Should(Succeed())
				}

				It("should re-use scheduler successfully", shouldeReUseDormantDBcheduler)

				Context("With Replica Set", func() {
					BeforeEach(func() {
						mongodb = f.MongoDBRS()
					})
					It("should take Snapshot successfully", shouldeReUseDormantDBcheduler)
				})
			})
		})

		FContext("Environment Variables", func() {

			Context("With allowed Envs", func() {
				BeforeEach(func() {
					mongodb.Spec.Init = &api.InitSpec{
						ScriptSource: &api.ScriptSourceSpec{
							VolumeSource: core.VolumeSource{
								GitRepo: &core.GitRepoVolumeSource{
									Repository: "https://github.com/kubedb/mongodb-init-scripts.git",
									Directory:  ".",
								},
							},
						},
					}
				})

				var withAllowedEnvs = func() {
					dbName = "envDB"
					mongodb.Spec.Env = []core.EnvVar{
						{
							Name:  MONGO_INITDB_DATABASE,
							Value: dbName,
						},
					}

					// Create MongoDB
					createAndWaitForRunning()

					By("Checking Inserted Document")
					f.EventuallyDocumentExists(mongodb.ObjectMeta, dbName).Should(BeTrue())
				}

				It("should initialize database specified by env", withAllowedEnvs)

				Context("With Replica Set", func() {
					BeforeEach(func() {
						mongodb = f.MongoDBRS()
						mongodb.Spec.Init = &api.InitSpec{
							ScriptSource: &api.ScriptSourceSpec{
								VolumeSource: core.VolumeSource{
									GitRepo: &core.GitRepoVolumeSource{
										Repository: "https://github.com/kubedb/mongodb-init-scripts.git",
										Directory:  ".",
									},
								},
							},
						}
					})
					It("should take Snapshot successfully", withAllowedEnvs)
				})

			})

			Context("With forbidden Envs", func() {

				var withForbiddenEnvs = func() {

					By("Create MongoDB with " + MONGO_INITDB_ROOT_USERNAME + " env var")
					mongodb.Spec.Env = []core.EnvVar{
						{
							Name:  MONGO_INITDB_ROOT_USERNAME,
							Value: "mg-user",
						},
					}
					err = f.CreateMongoDB(mongodb)
					Expect(err).To(HaveOccurred())

					By("Create MongoDB with " + MONGO_INITDB_ROOT_PASSWORD + " env var")
					mongodb.Spec.Env = []core.EnvVar{
						{
							Name:  MONGO_INITDB_ROOT_PASSWORD,
							Value: "not@secret",
						},
					}
					err = f.CreateMongoDB(mongodb)
					Expect(err).To(HaveOccurred())
				}

				It("should reject to create MongoDB crd", withForbiddenEnvs)

				Context("With Replica Set", func() {
					BeforeEach(func() {
						mongodb = f.MongoDBRS()
					})
					It("should take Snapshot successfully", withForbiddenEnvs)
				})
			})

			Context("Update Envs", func() {
				BeforeEach(func() {
					mongodb.Spec.Init = &api.InitSpec{
						ScriptSource: &api.ScriptSourceSpec{
							VolumeSource: core.VolumeSource{
								GitRepo: &core.GitRepoVolumeSource{
									Repository: "https://github.com/kubedb/mongodb-init-scripts.git",
									Directory:  ".",
								},
							},
						},
					}
				})

				var withUpdateEnvs = func() {

					dbName = "envDB"
					mongodb.Spec.Env = []core.EnvVar{
						{
							Name:  MONGO_INITDB_DATABASE,
							Value: dbName,
						},
					}

					// Create MongoDB
					createAndWaitForRunning()

					By("Checking Inserted Document")
					f.EventuallyDocumentExists(mongodb.ObjectMeta, dbName).Should(BeTrue())

					_, _, err = util.PatchMongoDB(f.ExtClient(), mongodb, func(in *api.MongoDB) *api.MongoDB {
						in.Spec.Env = []core.EnvVar{
							{
								Name:  MONGO_INITDB_DATABASE,
								Value: "patched-db",
							},
						}
						return in
					})

					Expect(err).To(HaveOccurred())
				}

				It("should initialize database specified by env", withUpdateEnvs)

				Context("With Replica Set", func() {
					BeforeEach(func() {
						mongodb = f.MongoDBRS()
						mongodb.Spec.Init = &api.InitSpec{
							ScriptSource: &api.ScriptSourceSpec{
								VolumeSource: core.VolumeSource{
									GitRepo: &core.GitRepoVolumeSource{
										Repository: "https://github.com/kubedb/mongodb-init-scripts.git",
										Directory:  ".",
									},
								},
							},
						}
					})

					It("should take Snapshot successfully", withUpdateEnvs)
				})
			})
		})
	})
})
