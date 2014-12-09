package instance_test

import (
	. "github.com/cloudfoundry/bosh-micro-cli/deployer/instance"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"errors"
	"time"

	boshlog "github.com/cloudfoundry/bosh-agent/logger"
	bmdisk "github.com/cloudfoundry/bosh-micro-cli/deployer/disk"
	bmsshtunnel "github.com/cloudfoundry/bosh-micro-cli/deployer/sshtunnel"
	bmstemcell "github.com/cloudfoundry/bosh-micro-cli/deployer/stemcell"
	bmdepl "github.com/cloudfoundry/bosh-micro-cli/deployment"
	bmeventlog "github.com/cloudfoundry/bosh-micro-cli/eventlogger"

	fakebmdisk "github.com/cloudfoundry/bosh-micro-cli/deployer/disk/fakes"
	fakebmsshtunnel "github.com/cloudfoundry/bosh-micro-cli/deployer/sshtunnel/fakes"
	fakebmvm "github.com/cloudfoundry/bosh-micro-cli/deployer/vm/fakes"
	fakebmlog "github.com/cloudfoundry/bosh-micro-cli/eventlogger/fakes"
)

var _ = Describe("Instance", func() {
	var (
		fakeVMManager        *fakebmvm.FakeManager
		fakeVM               *fakebmvm.FakeVM
		fakeSSHTunnelFactory *fakebmsshtunnel.FakeFactory
		fakeSSHTunnel        *fakebmsshtunnel.FakeTunnel
		fakeStage            *fakebmlog.FakeStage

		instance Instance

		pingTimeout = 1 * time.Second
		pingDelay   = 500 * time.Millisecond
	)

	BeforeEach(func() {
		fakeVMManager = fakebmvm.NewFakeManager()
		fakeVM = fakebmvm.NewFakeVM("fake-vm-cid")

		fakeSSHTunnelFactory = fakebmsshtunnel.NewFakeFactory()
		fakeSSHTunnel = fakebmsshtunnel.NewFakeTunnel()
		fakeSSHTunnel.SetStartBehavior(nil, nil)
		fakeSSHTunnelFactory.SSHTunnel = fakeSSHTunnel

		logger := boshlog.NewLogger(boshlog.LevelNone)

		instance = NewInstance("fake-job-name", 0, fakeVM, fakeVMManager, fakeSSHTunnelFactory, logger)

		fakeStage = fakebmlog.NewFakeStage()
	})

	Describe("Delete", func() {
		It("checks if the agent on the vm is responsive", func() {
			err := instance.Delete(pingTimeout, pingDelay, fakeStage)
			Expect(err).NotTo(HaveOccurred())

			Expect(fakeVM.WaitUntilReadyInputs).To(ContainElement(fakebmvm.WaitUntilReadyInput{
				Timeout: pingTimeout,
				Delay:   pingDelay,
			}))
		})

		It("deletes existing vm", func() {
			err := instance.Delete(pingTimeout, pingDelay, fakeStage)
			Expect(err).NotTo(HaveOccurred())

			Expect(fakeVM.DeleteCalled).To(Equal(1))
		})

		It("logs start and stop events", func() {
			err := instance.Delete(pingTimeout, pingDelay, fakeStage)
			Expect(err).NotTo(HaveOccurred())

			Expect(fakeStage.Steps).To(Equal([]*fakebmlog.FakeStep{
				{
					Name: "Waiting for the agent on VM 'fake-vm-cid'",
					States: []bmeventlog.EventState{
						bmeventlog.Started,
						bmeventlog.Finished,
					},
				},
				{
					Name: "Stopping jobs on instance 'fake-job-name/0'",
					States: []bmeventlog.EventState{
						bmeventlog.Started,
						bmeventlog.Finished,
					},
				},
				{
					Name: "Deleting VM 'fake-vm-cid'",
					States: []bmeventlog.EventState{
						bmeventlog.Started,
						bmeventlog.Finished,
					},
				},
			}))
		})

		Context("when agent is responsive", func() {
			It("logs waiting for the agent event", func() {
				err := instance.Delete(pingTimeout, pingDelay, fakeStage)
				Expect(err).ToNot(HaveOccurred())

				Expect(fakeStage.Steps).To(ContainElement(&fakebmlog.FakeStep{
					Name: "Waiting for the agent on VM 'fake-vm-cid'",
					States: []bmeventlog.EventState{
						bmeventlog.Started,
						bmeventlog.Finished,
					},
				}))
			})

			It("stops vm", func() {
				err := instance.Delete(pingTimeout, pingDelay, fakeStage)
				Expect(err).ToNot(HaveOccurred())

				Expect(fakeVM.StopCalled).To(Equal(1))
			})

			It("unmounts vm disks", func() {
				firstDisk := fakebmdisk.NewFakeDisk("fake-disk-1")
				secondDisk := fakebmdisk.NewFakeDisk("fake-disk-2")
				fakeVM.ListDisksDisks = []bmdisk.Disk{firstDisk, secondDisk}

				err := instance.Delete(pingTimeout, pingDelay, fakeStage)
				Expect(err).ToNot(HaveOccurred())

				Expect(fakeVM.UnmountDiskInputs).To(Equal([]fakebmvm.UnmountDiskInput{
					{Disk: firstDisk},
					{Disk: secondDisk},
				}))

				Expect(fakeStage.Steps).To(ContainElement(&fakebmlog.FakeStep{
					Name: "Unmounting disk 'fake-disk-1'",
					States: []bmeventlog.EventState{
						bmeventlog.Started,
						bmeventlog.Finished,
					},
				}))
				Expect(fakeStage.Steps).To(ContainElement(&fakebmlog.FakeStep{
					Name: "Unmounting disk 'fake-disk-2'",
					States: []bmeventlog.EventState{
						bmeventlog.Started,
						bmeventlog.Finished,
					},
				}))
			})

			Context("when stopping vm fails", func() {
				BeforeEach(func() {
					fakeVM.StopErr = errors.New("fake-stop-error")
				})

				It("returns an error", func() {
					err := instance.Delete(pingTimeout, pingDelay, fakeStage)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("fake-stop-error"))

					Expect(fakeStage.Steps).To(ContainElement(&fakebmlog.FakeStep{
						Name: "Stopping jobs on instance 'fake-job-name/0'",
						States: []bmeventlog.EventState{
							bmeventlog.Started,
							bmeventlog.Failed,
						},
						FailMessage: "fake-stop-error",
					}))
				})
			})

			Context("when unmounting disk fails", func() {
				BeforeEach(func() {
					fakeVM.ListDisksDisks = []bmdisk.Disk{fakebmdisk.NewFakeDisk("fake-disk")}
					fakeVM.UnmountDiskErr = errors.New("fake-unmount-error")
				})

				It("returns an error", func() {
					err := instance.Delete(pingTimeout, pingDelay, fakeStage)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("fake-unmount-error"))

					Expect(fakeStage.Steps).To(ContainElement(&fakebmlog.FakeStep{
						Name: "Unmounting disk 'fake-disk'",
						States: []bmeventlog.EventState{
							bmeventlog.Started,
							bmeventlog.Failed,
						},
						FailMessage: "Unmounting disk 'fake-disk' from VM 'fake-vm-cid': fake-unmount-error",
					}))
				})
			})
		})

		Context("when agent fails to respond", func() {
			BeforeEach(func() {
				fakeVM.WaitUntilReadyErr = errors.New("fake-wait-error")
			})

			It("logs failed event", func() {
				err := instance.Delete(pingTimeout, pingDelay, fakeStage)
				Expect(err).ToNot(HaveOccurred())

				Expect(fakeStage.Steps).To(ContainElement(&fakebmlog.FakeStep{
					Name: "Waiting for the agent on VM 'fake-vm-cid'",
					States: []bmeventlog.EventState{
						bmeventlog.Started,
						bmeventlog.Failed,
					},
					FailMessage: "Agent unreachable: fake-wait-error",
				}))
			})
		})

		Context("when deleting VM fails", func() {
			BeforeEach(func() {
				fakeVM.DeleteErr = errors.New("fake-delete-error")
			})

			It("returns an error", func() {
				err := instance.Delete(pingTimeout, pingDelay, fakeStage)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("fake-delete-error"))

				Expect(fakeStage.Steps).To(ContainElement(&fakebmlog.FakeStep{
					Name: "Deleting VM 'fake-vm-cid'",
					States: []bmeventlog.EventState{
						bmeventlog.Started,
						bmeventlog.Failed,
					},
					FailMessage: "Deleting VM: fake-delete-error",
				}))
			})
		})
	})

	Describe("StartJobs", func() {
		var (
			applySpec  bmstemcell.ApplySpec
			deployment bmdepl.Deployment
		)

		BeforeEach(func() {
			applySpec = bmstemcell.ApplySpec{
				Job: bmstemcell.Job{
					Name: "fake-job-name",
				},
			}

			deployment = bmdepl.Deployment{
				Update: bmdepl.Update{
					UpdateWatchTime: bmdepl.WatchTime{
						Start: 0,
						End:   5478,
					},
				},
				Jobs: []bmdepl.Job{
					{
						Name:               "fake-job-name",
						PersistentDiskPool: "fake-persistent-disk-pool-name",
						Instances:          1,
					},
				},
			}
		})

		It("tells the agent to start the jobs", func() {
			err := instance.StartJobs(applySpec, deployment, fakeStage)
			Expect(err).NotTo(HaveOccurred())

			Expect(fakeVM.StartCalled).To(Equal(1))
		})

		It("waits until agent reports state as running", func() {
			err := instance.StartJobs(applySpec, deployment, fakeStage)
			Expect(err).NotTo(HaveOccurred())

			Expect(fakeVM.WaitToBeRunningInputs).To(ContainElement(fakebmvm.WaitInput{
				MaxAttempts: 5,
				Delay:       1 * time.Second,
			}))
		})

		It("logs start and stop events to the eventLogger", func() {
			err := instance.StartJobs(applySpec, deployment, fakeStage)
			Expect(err).NotTo(HaveOccurred())

			Expect(fakeStage.Steps).To(ContainElement(&fakebmlog.FakeStep{
				Name: "Starting instance 'fake-job-name/0'",
				States: []bmeventlog.EventState{
					bmeventlog.Started,
					bmeventlog.Finished,
				},
			}))
			Expect(fakeStage.Steps).To(ContainElement(&fakebmlog.FakeStep{
				Name: "Waiting for instance 'fake-job-name/0' to be running",
				States: []bmeventlog.EventState{
					bmeventlog.Started,
					bmeventlog.Finished,
				},
			}))
		})

		Context("when updating instance state fails", func() {
			BeforeEach(func() {
				fakeVM.ApplyErr = errors.New("fake-apply-error")
			})

			It("logs start and stop events to the eventLogger", func() {
				err := instance.StartJobs(applySpec, deployment, fakeStage)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("fake-apply-error"))

				Expect(fakeStage.Steps).To(ContainElement(&fakebmlog.FakeStep{
					Name: "Starting instance 'fake-job-name/0'",
					States: []bmeventlog.EventState{
						bmeventlog.Started,
						bmeventlog.Failed,
					},
					FailMessage: "Applying the agent state: fake-apply-error",
				}))
			})
		})

		Context("when starting agent services fails", func() {
			BeforeEach(func() {
				fakeVM.StartErr = errors.New("fake-start-error")
			})

			It("logs start and stop events to the eventLogger", func() {
				err := instance.StartJobs(applySpec, deployment, fakeStage)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("fake-start-error"))

				Expect(fakeStage.Steps).To(ContainElement(&fakebmlog.FakeStep{
					Name: "Starting instance 'fake-job-name/0'",
					States: []bmeventlog.EventState{
						bmeventlog.Started,
						bmeventlog.Failed,
					},
					FailMessage: "Starting the agent: fake-start-error",
				}))
			})
		})

		Context("when waiting for running state fails", func() {
			BeforeEach(func() {
				fakeVM.WaitToBeRunningErr = errors.New("fake-wait-running-error")
			})

			It("logs start and stop events to the eventLogger", func() {
				err := instance.StartJobs(applySpec, deployment, fakeStage)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("fake-wait-running-error"))

				Expect(fakeStage.Steps).To(ContainElement(&fakebmlog.FakeStep{
					Name: "Waiting for instance 'fake-job-name/0' to be running",
					States: []bmeventlog.EventState{
						bmeventlog.Started,
						bmeventlog.Failed,
					},
					FailMessage: "fake-wait-running-error",
				}))
			})
		})
	})

	Describe("WaitUntilReady", func() {
		var (
			sshTunnelOptions bmsshtunnel.Options
		)

		BeforeEach(func() {
			sshTunnelOptions = bmsshtunnel.Options{
				Host:              "fake-ssh-host",
				Port:              124,
				User:              "fake-ssh-username",
				Password:          "fake-password",
				PrivateKey:        "fake-private-key-path",
				LocalForwardPort:  125,
				RemoteForwardPort: 126,
			}
		})

		It("starts & stops the SSH tunnel", func() {
			err := instance.WaitUntilReady(sshTunnelOptions, fakeStage)
			Expect(err).NotTo(HaveOccurred())
			Expect(fakeSSHTunnelFactory.NewSSHTunnelOptions).To(Equal(bmsshtunnel.Options{
				User:              "fake-ssh-username",
				PrivateKey:        "fake-private-key-path",
				Password:          "fake-password",
				Host:              "fake-ssh-host",
				Port:              124,
				LocalForwardPort:  125,
				RemoteForwardPort: 126,
			}))
			Expect(fakeSSHTunnel.Started).To(BeTrue())
			Expect(fakeSSHTunnel.Stopped).To(BeTrue())
		})

		It("waits for the vm", func() {
			err := instance.WaitUntilReady(sshTunnelOptions, fakeStage)
			Expect(err).NotTo(HaveOccurred())
			Expect(fakeVM.WaitUntilReadyInputs).To(ContainElement(fakebmvm.WaitUntilReadyInput{
				Timeout: 10 * time.Minute,
				Delay:   500 * time.Millisecond,
			}))
		})

		It("logs start and stop events to the eventLogger", func() {
			err := instance.WaitUntilReady(sshTunnelOptions, fakeStage)
			Expect(err).NotTo(HaveOccurred())

			Expect(fakeStage.Steps).To(ContainElement(&fakebmlog.FakeStep{
				Name: "Waiting for the agent on VM 'fake-vm-cid' to be ready",
				States: []bmeventlog.EventState{
					bmeventlog.Started,
					bmeventlog.Finished,
				},
			}))
		})

		Context("when ssh options are empty", func() {
			BeforeEach(func() {
				sshTunnelOptions = bmsshtunnel.Options{}
			})

			It("does not start ssh tunnel", func() {
				err := instance.WaitUntilReady(sshTunnelOptions, fakeStage)
				Expect(err).ToNot(HaveOccurred())
				Expect(fakeSSHTunnel.Started).To(BeFalse())
			})
		})

		Context("when starting SSH tunnel fails", func() {
			BeforeEach(func() {
				fakeSSHTunnel.SetStartBehavior(errors.New("fake-ssh-tunnel-start-error"), nil)
			})

			It("returns an error", func() {
				err := instance.WaitUntilReady(sshTunnelOptions, fakeStage)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("fake-ssh-tunnel-start-error"))
			})
		})

		Context("when waiting for the agent fails", func() {
			BeforeEach(func() {
				fakeVM.WaitUntilReadyErr = errors.New("fake-wait-error")
			})

			It("logs start and stop events to the eventLogger", func() {
				err := instance.WaitUntilReady(sshTunnelOptions, fakeStage)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("fake-wait-error"))

				Expect(fakeStage.Steps).To(ContainElement(&fakebmlog.FakeStep{
					Name: "Waiting for the agent on VM 'fake-vm-cid' to be ready",
					States: []bmeventlog.EventState{
						bmeventlog.Started,
						bmeventlog.Failed,
					},
					FailMessage: "fake-wait-error",
				}))
			})
		})
	})
})