/*
Copyright 2023 The KusionStack Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package pod

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	k8sErr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog/v2"
	utilpointer "k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh"
	"github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/constants"
	ctrlmeshv1alpha1 "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/v1alpha1"
	util "github.com/KusionStack/controller-mesh/pkg/utils"
)

var (
	initImage  = flag.String("init-image", "", "The image for ControllerMesh init container.")
	proxyImage = flag.String("proxy-image", "", "The image for ControllerMesh proxy container.")

	fakeKubeconfig = flag.String("fake-kubeconfig", "fake-kubeconfig", "The fake kubeconfig volume name")

	proxyImagePullPolicy          = flag.String("proxy-image-pull-policy", "Always", "Image pull policy for ControllerMesh proxy container, can be Always or IfNotPresent.")
	proxyResourceCPU              = flag.String("proxy-cpu", "500m", "The CPU limit for ControllerMesh proxy container.")
	proxyResourceMemory           = flag.String("proxy-memory", "1Gi", "The Memory limit for ControllerMesh proxy container.")
	proxyResourceEphemeralStorage = flag.String("proxy-ephemeral-storage", "2Gi", "The EphemeralStorage limit for ControllerMesh proxy container.")
	proxyLogLevel                 = flag.Uint("proxy-logv", 4, "The log level of ControllerMesh proxy container.")
	proxyExtraEnvs                = flag.String("proxy-extra-envs", "", "Extra environments for ControllerMesh proxy container.")

	fakeConfigMap = &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      *fakeKubeconfig,
			Namespace: "default",
			Labels: map[string]string{
				ctrlmesh.CtrlmeshWatchOnLimitLabel: "true",
			},
		},
		Data: map[string]string{"fake-kubeconfig.yaml": "apiVersion: v1\n" +
			"clusters:\n" +
			"  - cluster:\n" +
			"      insecure-skip-tls-verify: true\n" +
			"      server: http://127.0.0.1:5443\n" +
			"    name: fake-cluster\n" +
			"contexts:\n" +
			"  - context:\n" +
			"      cluster: fake-cluster\n" +
			"      namespace: fake-namespace\n" +
			"      user: fake-user\n" +
			"    name: fake-context\n" +
			"current-context: fake-context\n" +
			"kind: Config\n" +
			"preferences: {}\n" +
			"users:\n" +
			"  - name: fake-user\n" +
			"    user:\n" +
			"      username:\n" +
			"      password:\n"},
	}
)

func (h *MutatingHandler) injectByShardingConfig(ctx context.Context, pod *v1.Pod) (retErr error) {
	shardingConfigList := &ctrlmeshv1alpha1.ShardingConfigList{}
	if err := h.Client.List(ctx, shardingConfigList, client.InNamespace(pod.Namespace)); err != nil {
		return err
	}

	var matchedCfg *ctrlmeshv1alpha1.ShardingConfig
	for i := range shardingConfigList.Items {
		if shardingConfigList.Items[i].Spec.Root != nil {
			continue
		}
		cfg := &shardingConfigList.Items[i]
		selector, err := metav1.LabelSelectorAsSelector(cfg.Spec.Selector)
		if err != nil {
			klog.Warningf("Failed to convert selector for ShardingConfig %s/%s: %v", cfg.Namespace, cfg.Name, err)
			continue
		}
		if selector.Matches(labels.Set(pod.Labels)) {
			if matchedCfg != nil {
				klog.Warningf("Find multiple ShardingConfig %s %s matched Pod %s/%s", matchedCfg.Name, cfg.Name, pod.Namespace, pod.Name)
				return fmt.Errorf("multiple ShardingConfig %s %s matched", matchedCfg.Name, cfg.Name)
			}
			matchedCfg = cfg
		}
	}
	if matchedCfg == nil {
		return fmt.Errorf("can not find ShardingConfig to inject proxy container, if don't need it please disable ctrlmesh webhook label")
	}

	var initContainer *v1.Container
	var proxyContainer *v1.Container
	defer func() {
		if retErr == nil {
			klog.Infof("Successfully inject ShardingConfig %s for Pod %s/%s creation, init: %s, sidecar: %s",
				matchedCfg.Name, pod.Namespace, pod.Name, util.DumpJSON(initContainer), util.DumpJSON(proxyContainer))
		} else {
			klog.Warningf("Failed to inject ShardingConfig %s for Pod %s/%s creation, error: %v",
				matchedCfg.Name, pod.Namespace, pod.Name, retErr)
		}
	}()

	if pod.Spec.HostNetwork {
		return fmt.Errorf("can not use ControllerMesh for Pod with host network")
	}
	if *proxyImage == "" {
		return fmt.Errorf("the images for ControllerMesh init or proxy container have not set in args")
	}

	imagePullPolicy := v1.PullAlways
	if *proxyImagePullPolicy == string(v1.PullIfNotPresent) {
		imagePullPolicy = v1.PullIfNotPresent
	}

	proxyContainer = &v1.Container{
		Name:            constants.ProxyContainerName,
		Image:           *proxyImage,
		ImagePullPolicy: imagePullPolicy,
		Args:            []string{
			//"--v=" + strconv.Itoa(int(*proxyLogLevel)),
		},
		Env: []v1.EnvVar{
			{Name: constants.EnvPodName, ValueFrom: &v1.EnvVarSource{FieldRef: &v1.ObjectFieldSelector{FieldPath: "metadata.name"}}},
			{Name: constants.EnvPodNamespace, ValueFrom: &v1.EnvVarSource{FieldRef: &v1.ObjectFieldSelector{FieldPath: "metadata.namespace"}}},
			{Name: constants.EnvPodIP, ValueFrom: &v1.EnvVarSource{FieldRef: &v1.ObjectFieldSelector{FieldPath: "status.podIP"}}},
		},
		Lifecycle: &v1.Lifecycle{
			PostStart: &v1.LifecycleHandler{
				Exec: &v1.ExecAction{Command: []string{"/bin/sh", "-c", "/poststart.sh"}},
			},
		},
		ReadinessProbe: &v1.Probe{
			ProbeHandler:  v1.ProbeHandler{HTTPGet: &v1.HTTPGetAction{Path: "/readyz", Port: intstr.FromInt(constants.ProxyMetricsHealthPort)}},
			PeriodSeconds: 3,
		},
		Resources: v1.ResourceRequirements{
			Limits: v1.ResourceList{
				v1.ResourceCPU:              resource.MustParse(*proxyResourceCPU),
				v1.ResourceMemory:           resource.MustParse(*proxyResourceMemory),
				v1.ResourceEphemeralStorage: resource.MustParse(*proxyResourceEphemeralStorage),
			},
			Requests: v1.ResourceList{
				v1.ResourceCPU:              resource.MustParse(*proxyResourceCPU),
				v1.ResourceMemory:           resource.MustParse(*proxyResourceMemory),
				v1.ResourceEphemeralStorage: resource.MustParse(*proxyResourceEphemeralStorage),
			},
		},
		SecurityContext: &v1.SecurityContext{
			Privileged: utilpointer.Bool(true), // This can be false, but true help us debug more easier.
			//ReadOnlyRootFilesystem: utilpointer.Bool(true),
			RunAsUser: utilpointer.Int64(int64(constants.ProxyUserID)),
		},
	}

	if val, ok := pod.Annotations[ctrlmesh.CtrlmeshProxyContainerResourceAnno]; ok {
		req := &v1.ResourceRequirements{}
		if err := json.Unmarshal([]byte(val), req); err != nil {
			klog.Errorf("fail to unmarshal resource requirements %v", err)
		} else {
			proxyContainer.Resources = *req
		}
	}

	initContainer = &v1.Container{
		Name:            constants.InitContainerName,
		Image:           *initImage,
		ImagePullPolicy: imagePullPolicy,
		SecurityContext: &v1.SecurityContext{
			Privileged:   utilpointer.Bool(true),
			Capabilities: &v1.Capabilities{Add: []v1.Capability{"NET_ADMIN"}},
		},
	}

	if envs := getExtraEnvs(); len(envs) > 0 {
		proxyContainer.Env = append(proxyContainer.Env, envs...)
	}

	if annoEnv := getEnvFromAnno(pod); len(annoEnv) > 0 {
		proxyContainer.Env = append(proxyContainer.Env, annoEnv...)
	}

	apiserverHostPortEnvs, err := getKubernetesServiceHostPort(pod)
	if err != nil {
		return err
	}
	if len(apiserverHostPortEnvs) > 0 {
		initContainer.Env = append(initContainer.Env, apiserverHostPortEnvs...)
		proxyContainer.Env = append(proxyContainer.Env, apiserverHostPortEnvs...)
	}

	ipTableEnvs := getEnv(pod, constants.EnvIPTable)
	enableIpTable := false
	if len(ipTableEnvs) > 0 {
		initContainer.Env = append(initContainer.Env, ipTableEnvs...)
		//proxyContainer.Env = append(proxyContainer.Env, ipTableEnvs...)
		if ipTableEnvs[0].Value == "true" {
			enableIpTable = true
		}
	}
	if !enableIpTable {
		if err := h.applyFakeConfigMap(pod); err != nil {
			return err
		}
		if err := mountFakeKubeConfig(pod, *fakeKubeconfig); err != nil {
			return err
		}
	}

	if envKeys := constants.AllProxySyncEnvKey(); len(envKeys) > 0 {
		for _, key := range envKeys {
			ev := getEnv(pod, key)
			if len(ev) > 0 {
				proxyContainer.Env = append(proxyContainer.Env, ev...)
			}
		}
	}

	webhookEnv := getEnv(pod, constants.EnvEnableWebHookProxy)
	if len(webhookEnv) > 0 {
		initContainer.Env = append(initContainer.Env, webhookEnv...)
	}

	if matchedCfg.Spec.Controller != nil {
		proxyContainer.Args = append(
			proxyContainer.Args,
			fmt.Sprintf("--%s=%v", constants.ProxyLeaderElectionNameFlag, matchedCfg.Spec.Controller.LeaderElectionName),
		)
	}

	if matchedCfg.Spec.Webhook != nil {
		initContainer.Env = append(
			initContainer.Env,
			v1.EnvVar{Name: constants.EnvInboundWebhookPort, Value: strconv.Itoa(matchedCfg.Spec.Webhook.Port)},
		)
		proxyContainer.Args = append(
			proxyContainer.Args,
			fmt.Sprintf("--%s=%v", constants.ProxyWebhookCertDirFlag, matchedCfg.Spec.Webhook.CertDir),
			fmt.Sprintf("--%s=%v", constants.ProxyWebhookServePortFlag, matchedCfg.Spec.Webhook.Port),
		)
		certVolumeMounts := getVolumeMountsWithDir(pod, matchedCfg.Spec.Webhook.CertDir)
		if len(certVolumeMounts) > 1 {
			return fmt.Errorf("find multiple volume mounts that mount at %s: %v", matchedCfg.Spec.Webhook.CertDir, certVolumeMounts)
		} else if len(certVolumeMounts) == 0 {
			return fmt.Errorf("find no volume mounts that mount at %s", matchedCfg.Spec.Webhook.CertDir)
		} else {
			proxyContainer.VolumeMounts = append(proxyContainer.VolumeMounts, certVolumeMounts[0])
		}
	}
	if *initImage != "" {
		pod.Spec.InitContainers = append([]v1.Container{*initContainer}, pod.Spec.InitContainers...)
	}
	if pod.Labels == nil {
		pod.Labels = map[string]string{}
	}

	if volumeName, ok := pod.Labels[ctrlmesh.CtrlmeshProxyKubeConfigVolumeLabel]; ok {
		pickVolume(pod, volumeName)
		var cmName string
		for _, vol := range pod.Spec.Volumes {
			if vol.Name != volumeName {
				continue
			}
			if vol.ConfigMap == nil {
				data, _ := json.Marshal(vol)
				return fmt.Errorf(fmt.Sprintf("get kubeconfig from volume %s error: only support configmap volume, %s", volumeName, string(data)))
			}
			cmName = vol.ConfigMap.Name
			break
		}
		kubeconfigCm, err := h.directKubeClient.CoreV1().ConfigMaps(pod.Namespace).Get(context.TODO(), cmName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if len(kubeconfigCm.Data) > 1 {
			return fmt.Errorf(fmt.Sprintf("kubeconfig config map %s data size > 1", cmName))
		}
		var kubeconfigFileName string
		for k := range kubeconfigCm.Data {
			kubeconfigFileName = k
			break
		}
		proxyContainer.Args = append(proxyContainer.Args, fmt.Sprintf("--kubeconfig=/etc/kubernetes/kubeconfig/%s", kubeconfigFileName))
		proxyContainer.VolumeMounts = append(proxyContainer.VolumeMounts, v1.VolumeMount{
			Name:      volumeName,
			ReadOnly:  true,
			MountPath: "/etc/kubernetes/kubeconfig",
		})
	}

	pod.Spec.Containers = append([]v1.Container{*proxyContainer}, pod.Spec.Containers...)
	injectEnv(pod)
	pod.Labels[ctrlmeshv1alpha1.ShardingConfigInjectedKey] = matchedCfg.Name
	if logPath, find := pod.Annotations[ctrlmesh.CtrlmeshSharedLogPathAnno]; find {
		return mountLogVolume(pod, logPath, "app-shared-logs")
	}
	return nil
}

func pickVolume(po *v1.Pod, name string) {
	for _, c := range po.Spec.Containers {
		for i, v := range c.VolumeMounts {
			if v.Name == name {
				c.VolumeMounts = append(c.VolumeMounts[:i], c.VolumeMounts[i+1:]...)
				break
			}
		}
	}
}

func (h *MutatingHandler) applyFakeConfigMap(po *v1.Pod) error {
	if _, err := h.directKubeClient.CoreV1().ConfigMaps(po.Namespace).Get(context.TODO(), *fakeKubeconfig, metav1.GetOptions{}); err != nil {
		if !k8sErr.IsNotFound(err) {
			return fmt.Errorf("fail to get configMap fake-kubeconfig %s", err)
		}
		cm := fakeConfigMap.DeepCopy()
		cm.Namespace = po.Namespace
		err = h.Client.Create(context.TODO(), cm)
		if err != nil && !k8sErr.IsAlreadyExists(err) && !k8sErr.IsConflict(err) {
			return fmt.Errorf("fail to apply configMap fake-kubeconfig %s", err)
		}
	}
	return nil
}

func getEnv(pod *v1.Pod, key string) (vars []v1.EnvVar) {
	var ipEnv *v1.EnvVar
	for i := range pod.Spec.Containers {
		if envVar := util.GetContainerEnvVar(&pod.Spec.Containers[i], key); envVar != nil {
			ipEnv = envVar
			break
		}
	}
	if ipEnv != nil {
		vars = append(vars, *ipEnv)
	}
	return vars
}

func getEnvFromAnno(pod *v1.Pod) (vars []v1.EnvVar) {
	if pod.Annotations == nil {
		return
	}
	envStr, ok := pod.Annotations[ctrlmesh.CtrlmeshWebhookEnvConfigAnno]
	if !ok {
		return
	}
	envs := strings.Split(envStr, ",")
	for _, e := range envs {
		if e == "" {
			continue
		}
		env := strings.Split(e, "=")
		if len(env) > 1 {
			vars = append(vars, v1.EnvVar{
				Name:  env[0],
				Value: env[1],
			})
			continue
		}
		if val := getEnvValFromPod(pod, env[0]); val != "" {
			vars = append(vars, v1.EnvVar{
				Name:  env[0],
				Value: val,
			})
		}
	}
	return
}

func injectEnv(pod *v1.Pod) {
	if pod.Annotations == nil {
		return
	}
	envStr, ok := pod.Annotations[ctrlmesh.CtrlmeshEnvInjectAnno]
	if !ok {
		return
	}
	var envs []ContainerEnv
	if err := json.Unmarshal([]byte(envStr), &envs); err != nil {
		klog.Errorf("failed to unmarshal env: %s, %v", envStr, err)
		return
	}
	for _, containerEnv := range envs {
		//containerEnv.Name
		for i, container := range pod.Spec.Containers {
			if container.Name != containerEnv.Name {
				continue
			}
			mergeContainerEnvs(&pod.Spec.Containers[i], containerEnv.Env)
		}
	}
}

func mergeContainerEnvs(container *v1.Container, envs []Env) {
	if container.Env == nil {
		container.Env = []v1.EnvVar{}
	}
	var newEnvs []v1.EnvVar
	for _, env := range envs {
		find := false
		for i, containerEnv := range container.Env {
			if containerEnv.Name == env.Key {
				find = true
				container.Env[i].Value = env.Value
				break
			}
		}
		if !find {
			newEnvs = append(newEnvs, v1.EnvVar{Name: env.Key, Value: env.Value})
		}
	}
	if len(newEnvs) != 0 {
		container.Env = append(container.Env, newEnvs...)
	}
}

type ContainerEnv struct {
	Name string `json:"name"`
	Env  []Env  `json:"env"`
}

type Env struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func getEnvValFromPod(pod *v1.Pod, key string) string {
	for _, container := range pod.Spec.Containers {
		for _, contEnv := range container.Env {
			if contEnv.Name == key {
				return contEnv.Value
			}
		}
	}
	return ""
}

func mountLogVolume(pod *v1.Pod, path string, name string) error {
	existLogPath := func() bool {
		for i := range pod.Spec.Containers {
			for _, mo := range pod.Spec.Containers[i].VolumeMounts {
				if mo.MountPath == path {
					name = mo.Name
					return true
				}
			}
		}
		return false
	}()

	finVL := false
	for _, vl := range pod.Spec.Volumes {
		if vl.Name == name {
			finVL = true
			break
		}
	}
	vm := &v1.VolumeMount{
		Name:      name,
		MountPath: path,
	}
	vol := &v1.Volume{
		Name: name,
		VolumeSource: v1.VolumeSource{
			EmptyDir: &v1.EmptyDirVolumeSource{},
		},
	}

	if pod.Spec.Volumes == nil {
		pod.Spec.Volumes = []v1.Volume{}
	}

	if !finVL && !existLogPath {
		pod.Spec.Volumes = append(pod.Spec.Volumes, *vol)
	}
	for i := range pod.Spec.Containers {
		findVM := false
		for _, mo := range pod.Spec.Containers[i].VolumeMounts {
			if mo.Name == name {
				findVM = true
				break
			}
		}
		if !findVM {
			pod.Spec.Containers[i].VolumeMounts = append(pod.Spec.Containers[i].VolumeMounts, *vm.DeepCopy())
		}
	}
	return nil
}

func mountFakeKubeConfig(pod *v1.Pod, name string) error {
	vm := &v1.VolumeMount{
		Name:      name,
		MountPath: "/etc/kubernetes/kubeconfig",
		ReadOnly:  true,
	}
	mod := int32(420)
	fakeVol := &v1.Volume{
		Name: name,
		VolumeSource: v1.VolumeSource{
			ConfigMap: &v1.ConfigMapVolumeSource{
				LocalObjectReference: v1.LocalObjectReference{Name: "fake-kubeconfig"},
				DefaultMode:          &mod},
		},
	}
	if pod.Spec.Volumes == nil {
		pod.Spec.Volumes = []v1.Volume{}
	}
	findCm := false
	for _, vl := range pod.Spec.Volumes {
		if vl.Name == name {
			if vl.ConfigMap == nil || vl.ConfigMap.Name != name {
				return fmt.Errorf("conflict volume of %s", name)
			}
			findCm = true
		}
	}
	if !findCm {
		pod.Spec.Volumes = append(pod.Spec.Volumes, *fakeVol)
	}
	disableArg := pod.Labels[ctrlmesh.CtrlmeshDisableFakeKubeconfigArgLabel]
	disableEnv := pod.Labels[ctrlmesh.CtrlmeshDisableFakeKubeconfigEnvLabel]
	for i := range pod.Spec.Containers {
		findVol := false
		for _, mo := range pod.Spec.Containers[i].VolumeMounts {
			if mo.Name == name {
				findVol = true
			}
		}
		if !findVol {
			pod.Spec.Containers[i].VolumeMounts = append(pod.Spec.Containers[i].VolumeMounts, *vm.DeepCopy())
		}
		hvArgs := false
		for id, val := range pod.Spec.Containers[i].Args {
			if disableArg == "true" {
				break
			}
			if strings.HasPrefix(val, "--kubeconfig") || strings.HasPrefix(val, "-kubeconfig") {
				hvArgs = true
				pod.Spec.Containers[i].Args[id] = fmt.Sprintf("--kubeconfig=/etc/kubernetes/kubeconfig/%s.yaml", name)
			}
		}
		if !hvArgs {
			pod.Spec.Containers[i].Args = append(pod.Spec.Containers[i].Args, fmt.Sprintf("--kubeconfig=/etc/kubernetes/kubeconfig/%s.yaml", name))
		}
		findKubeconfigEnv := false
		for k, env := range pod.Spec.Containers[i].Env {
			if disableEnv == "true" {
				break
			}
			if env.Name == "KUBECONFIG" {
				pod.Spec.Containers[i].Env[k].Value = fmt.Sprintf("/etc/kubernetes/kubeconfig/%s.yaml", name)
				findKubeconfigEnv = true
			}
		}
		if !findKubeconfigEnv && disableEnv != "true" {
			pod.Spec.Containers[i].Env = append(pod.Spec.Containers[i].Env, v1.EnvVar{
				Name:  "KUBECONFIG",
				Value: fmt.Sprintf("/etc/kubernetes/kubeconfig/%s.yaml", name),
			})
		}
	}
	return nil
}

func getKubernetesServiceHostPort(pod *v1.Pod) (vars []v1.EnvVar, err error) {
	var hostEnv *v1.EnvVar
	var portEnv *v1.EnvVar
	for i := range pod.Spec.Containers {
		if envVar := util.GetContainerEnvVar(&pod.Spec.Containers[i], "KUBERNETES_SERVICE_HOST"); envVar != nil {
			if hostEnv != nil && hostEnv.Value != envVar.Value {
				return nil, fmt.Errorf("found multiple KUBERNETES_SERVICE_HOST values: %v, %v", hostEnv.Value, envVar.Value)
			}
			hostEnv = envVar
		}
		if envVar := util.GetContainerEnvVar(&pod.Spec.Containers[i], "KUBERNETES_SERVICE_PORT"); envVar != nil {
			if portEnv != nil && portEnv.Value != envVar.Value {
				return nil, fmt.Errorf("found multiple KUBERNETES_SERVICE_PORT values: %v, %v", portEnv.Value, envVar.Value)
			}
			portEnv = envVar
		}
	}
	if hostEnv != nil {
		vars = append(vars, *hostEnv)
	}
	if portEnv != nil {
		vars = append(vars, *portEnv)
	}
	return vars, nil
}

func getVolumeMountsWithDir(pod *v1.Pod, cerDir string) (mounts []v1.VolumeMount) {
	for i := range pod.Spec.Containers {
		c := &pod.Spec.Containers[i]

		for _, m := range c.VolumeMounts {
			if m.MountPath == cerDir {
				// do not modify the ref
				m.ReadOnly = true
				mounts = append(mounts, m)
			}
		}
	}
	return
}

func getExtraEnvs() (envs []v1.EnvVar) {
	if len(*proxyExtraEnvs) == 0 {
		return
	}
	kvs := strings.Split(*proxyExtraEnvs, ";")
	for _, str := range kvs {
		kv := strings.Split(str, "=")
		if len(kv) != 2 {
			continue
		}
		envs = append(envs, v1.EnvVar{Name: kv[0], Value: kv[1]})
	}
	return
}
