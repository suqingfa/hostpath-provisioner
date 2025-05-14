package main

import (
	"context"
	"os"
	"path"

	"github.com/pkg/errors"

	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"sigs.k8s.io/sig-storage-lib-external-provisioner/v6/controller"
)

const (
	defaultProvisionerName = "hostpath"
	defaultPVDir           = "/data"
	defaultIdentity        = "hostpath-provisioner"
)

var (
	provisionerName = getEnv("PROVISIONER_NAME", defaultProvisionerName)
	pvDir           = getEnv("PROVISIONER_DIR", defaultPVDir)
	identity        = getEnv("PROVISIONER_ISSUER", defaultIdentity)
)

func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	} else {
		return value
	}
}

type hostPathProvisioner struct {
	pvDir    string
	identity string
}

func (p *hostPathProvisioner) Provision(_ context.Context, options controller.ProvisionOptions) (*core.PersistentVolume, controller.ProvisioningState, error) {
	pvPath := path.Join(p.pvDir, options.PVC.Namespace, options.PVC.Name)

	klog.Infof("Provisioning volume %s for %v", pvPath, options)

	if err := os.MkdirAll(pvPath, 0777); err != nil && !os.IsExist(err) {
		klog.Errorf("Failed to create directory %s: %v", pvPath, err)
		return nil, controller.ProvisioningFinished, err
	}

	if err := os.Chmod(pvPath, 0777); err != nil {
		klog.Errorf("Failed to chmod directory %s: %v", pvPath, err)
		return nil, controller.ProvisioningFinished, err
	}

	pv := &core.PersistentVolume{
		ObjectMeta: meta.ObjectMeta{
			Name: options.PVName,
			Annotations: map[string]string{
				"hostPathProvisionerIdentity": string(p.identity),
			},
		},
		Spec: core.PersistentVolumeSpec{
			PersistentVolumeReclaimPolicy: *options.StorageClass.ReclaimPolicy,
			AccessModes:                   options.PVC.Spec.AccessModes,
			Capacity: core.ResourceList{
				core.ResourceStorage: options.PVC.Spec.Resources.Requests[core.ResourceStorage],
			},
			PersistentVolumeSource: core.PersistentVolumeSource{
				HostPath: &core.HostPathVolumeSource{
					Path: pvPath,
				},
			},
		},
	}

	return pv, controller.ProvisioningFinished, nil
}

func (p *hostPathProvisioner) Delete(_ context.Context, volume *core.PersistentVolume) error {
	klog.Infof("Deleting volume %v", volume)
	ann, ok := volume.Annotations["hostPathProvisionerIdentity"]
	if !ok {
		return errors.New("identity annotation not found on PV")
	}
	if ann != p.identity {
		return &controller.IgnoredError{Reason: "identity annotation on PV does not match ours"}
	}

	if err := os.RemoveAll(volume.Spec.PersistentVolumeSource.HostPath.Path); err != nil {
		return errors.Wrap(err, "removing hostpath PV")
	}

	return nil
}

func main() {
	klog.Infof("Initializing the storage provisioner...")

	config, err := rest.InClusterConfig()
	if err != nil {
		klog.Fatal(err)
		return
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.Fatalf("Failed to create client: %v", err)
		return
	}

	serverVersion, err := client.Discovery().ServerVersion()
	if err != nil {
		klog.Fatalf("error getting server version: %v", err)
		return
	}

	provisioner := &hostPathProvisioner{
		pvDir:    pvDir,
		identity: identity,
	}

	provisionController := controller.NewProvisionController(client, provisionerName, provisioner, serverVersion.GitVersion)

	klog.Info("Storage provisioner initialized, now starting service!")
	provisionController.Run(context.Background())
}
