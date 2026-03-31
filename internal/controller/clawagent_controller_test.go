/*
Copyright 2026.

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

package controller

import (
	"encoding/json"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clawv1 "github.com/clawbernetes/operator/api/v1"
)

/*
 Assumptions
 -----------
 These tests use envtest (a real kube-apiserver + etcd, no kubelet).
 This means:
   - CRDs, ConfigMaps, PVCs, Deployments, Services, and Secrets can be
     created/read/updated/deleted normally.
   - Pods are NEVER actually scheduled. Deployment status stays empty, so
     agent phase will always resolve to "Pending" — we don't assert on
     Running/Progressing because no kubelet exists to report conditions.
   - PVCs are accepted by the API but never bound (no provisioner). We
     verify the PVC object exists with correct spec, not that storage is
     provisioned.
   - Secret volumes are declared on the Deployment spec but never mounted
     to a real filesystem.

 What we DO verify:
   1. Ephemeral workspace → emptyDir volume (no PVC created)
   2. Persistent workspace → PVC created with correct size/class, volume
      switches to PersistentVolumeClaim source
   3. Credentials secret → secret volume mounted read-only at the correct
      path with mode 0400, auto-deny patterns injected in observeclaw config
   4. No credentials secret → no secret volume, no deny patterns
   5. Agent pod has NO EnvFrom/SecretRef (API key removed)
   6. openclaw.json has NO apiKey field
   7. Status.WorkspacePVC set when persistent, empty when ephemeral
*/

var _ = Describe("ClawAgent Controller — Workspace & Credential Security", func() {

	const ns = "default"

	reconcileAgent := func(name string) {
		r := &ClawAgentReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: name, Namespace: ns},
		})
		Expect(err).NotTo(HaveOccurred())
	}

	getDeployment := func(name string) *appsv1.Deployment {
		dep := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, dep)).To(Succeed())
		return dep
	}

	getConfigMap := func(name string) *corev1.ConfigMap {
		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, cm)).To(Succeed())
		return cm
	}

	mainContainer := func(dep *appsv1.Deployment) corev1.Container {
		for _, c := range dep.Spec.Template.Spec.Containers {
			if c.Name == "openclaw" {
				return c
			}
		}
		Fail("openclaw container not found in deployment")
		return corev1.Container{}
	}

	findVolume := func(dep *appsv1.Deployment, name string) *corev1.Volume {
		for i := range dep.Spec.Template.Spec.Volumes {
			if dep.Spec.Template.Spec.Volumes[i].Name == name {
				return &dep.Spec.Template.Spec.Volumes[i]
			}
		}
		return nil
	}

	findVolumeMount := func(c corev1.Container, name string) *corev1.VolumeMount {
		for i := range c.VolumeMounts {
			if c.VolumeMounts[i].Name == name {
				return &c.VolumeMounts[i]
			}
		}
		return nil
	}

	deleteIfExists := func(obj client.Object, key types.NamespacedName) {
		err := k8sClient.Get(ctx, key, obj)
		if err == nil {
			_ = k8sClient.Delete(ctx, obj)
		}
	}

	// -----------------------------------------------------------------------
	// Test: Ephemeral workspace (default)
	// -----------------------------------------------------------------------
	Context("with ephemeral workspace (default)", func() {
		const agentName = "test-ephemeral"

		BeforeEach(func() {
			agent := &clawv1.ClawAgent{
				ObjectMeta: metav1.ObjectMeta{Name: agentName, Namespace: ns},
				Spec: clawv1.ClawAgentSpec{
					Workspace: clawv1.WorkspaceSpec{Mode: clawv1.WorkspaceModeEphemeral},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).To(Succeed())
			reconcileAgent(agentName)
		})

		AfterEach(func() {
			deleteIfExists(&clawv1.ClawAgent{}, types.NamespacedName{Name: agentName, Namespace: ns})
		})

		It("should use emptyDir for openclaw-home volume", func() {
			dep := getDeployment(agentName)
			vol := findVolume(dep, "openclaw-home")
			Expect(vol).NotTo(BeNil())
			Expect(vol.VolumeSource.EmptyDir).NotTo(BeNil(), "expected emptyDir volume source")
			Expect(vol.VolumeSource.PersistentVolumeClaim).To(BeNil(), "should NOT have PVC volume source")
		})

		It("should NOT create a PVC", func() {
			pvc := &corev1.PersistentVolumeClaim{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: agentName + clawv1.PVCSuffix, Namespace: ns}, pvc)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "PVC should not exist for ephemeral workspace")
		})

		It("should set WorkspacePVC to empty in status", func() {
			agent := &clawv1.ClawAgent{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: agentName, Namespace: ns}, agent)).To(Succeed())
			Expect(agent.Status.WorkspacePVC).To(BeEmpty())
		})
	})

	// -----------------------------------------------------------------------
	// Test: Persistent workspace
	// -----------------------------------------------------------------------
	Context("with persistent workspace", func() {
		const agentName = "test-persistent"

		BeforeEach(func() {
			agent := &clawv1.ClawAgent{
				ObjectMeta: metav1.ObjectMeta{Name: agentName, Namespace: ns},
				Spec: clawv1.ClawAgentSpec{
					Workspace: clawv1.WorkspaceSpec{
						Mode:        clawv1.WorkspaceModePersistent,
						StorageSize: "8Gi",
					},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).To(Succeed())
			reconcileAgent(agentName)
		})

		AfterEach(func() {
			deleteIfExists(&clawv1.ClawAgent{}, types.NamespacedName{Name: agentName, Namespace: ns})
			deleteIfExists(&corev1.PersistentVolumeClaim{}, types.NamespacedName{Name: agentName + clawv1.PVCSuffix, Namespace: ns})
		})

		It("should create a PVC with the correct size", func() {
			pvc := &corev1.PersistentVolumeClaim{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: agentName + clawv1.PVCSuffix, Namespace: ns}, pvc)).To(Succeed())

			storageReq := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
			Expect(storageReq.String()).To(Equal("8Gi"))
			Expect(pvc.Spec.AccessModes).To(ContainElement(corev1.ReadWriteOnce))
		})

		It("should use PVC volume source on the deployment", func() {
			dep := getDeployment(agentName)
			vol := findVolume(dep, "openclaw-home")
			Expect(vol).NotTo(BeNil())
			Expect(vol.VolumeSource.PersistentVolumeClaim).NotTo(BeNil(), "expected PVC volume source")
			Expect(vol.VolumeSource.PersistentVolumeClaim.ClaimName).To(Equal(agentName + clawv1.PVCSuffix))
			Expect(vol.VolumeSource.EmptyDir).To(BeNil(), "should NOT have emptyDir")
		})

		It("should default PVC size to 5Gi when not specified", func() {
			// Create a second agent with no storageSize
			name2 := "test-persistent-default"
			agent2 := &clawv1.ClawAgent{
				ObjectMeta: metav1.ObjectMeta{Name: name2, Namespace: ns},
				Spec: clawv1.ClawAgentSpec{
					Workspace: clawv1.WorkspaceSpec{Mode: "persistent"},
				},
			}
			Expect(k8sClient.Create(ctx, agent2)).To(Succeed())
			reconcileAgent(name2)

			pvc := &corev1.PersistentVolumeClaim{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name2 + clawv1.PVCSuffix, Namespace: ns}, pvc)).To(Succeed())
			storageReq := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
			Expect(storageReq.String()).To(Equal("5Gi"))

			// Cleanup
			deleteIfExists(&clawv1.ClawAgent{}, types.NamespacedName{Name: name2, Namespace: ns})
			deleteIfExists(&corev1.PersistentVolumeClaim{}, types.NamespacedName{Name: name2 + clawv1.PVCSuffix, Namespace: ns})
		})

		It("should set WorkspacePVC in status", func() {
			agent := &clawv1.ClawAgent{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: agentName, Namespace: ns}, agent)).To(Succeed())
			Expect(agent.Status.WorkspacePVC).To(Equal(agentName + clawv1.PVCSuffix))
		})
	})

	// -----------------------------------------------------------------------
	// Test: reclaimPolicy=retain (default) — PVC has no owner reference
	// -----------------------------------------------------------------------
	Context("persistent workspace with reclaimPolicy=retain (default)", func() {
		const agentName = "test-retain"

		BeforeEach(func() {
			agent := &clawv1.ClawAgent{
				ObjectMeta: metav1.ObjectMeta{Name: agentName, Namespace: ns},
				Spec: clawv1.ClawAgentSpec{
					Workspace: clawv1.WorkspaceSpec{
						Mode:        clawv1.WorkspaceModePersistent,
						StorageSize: "1Gi",
					},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).To(Succeed())
			reconcileAgent(agentName)
		})

		AfterEach(func() {
			deleteIfExists(&clawv1.ClawAgent{}, types.NamespacedName{Name: agentName, Namespace: ns})
			deleteIfExists(&corev1.PersistentVolumeClaim{}, types.NamespacedName{Name: agentName + clawv1.PVCSuffix, Namespace: ns})
		})

		It("should create PVC without owner reference", func() {
			pvc := &corev1.PersistentVolumeClaim{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: agentName + clawv1.PVCSuffix, Namespace: ns}, pvc)).To(Succeed())
			Expect(pvc.OwnerReferences).To(BeEmpty(), "retain PVC must have no owner references")
		})
	})

	// -----------------------------------------------------------------------
	// Test: reclaimPolicy=delete — PVC has owner reference (GC'd with agent)
	// -----------------------------------------------------------------------
	Context("persistent workspace with reclaimPolicy=delete", func() {
		const agentName = "test-delete-policy"

		BeforeEach(func() {
			agent := &clawv1.ClawAgent{
				ObjectMeta: metav1.ObjectMeta{Name: agentName, Namespace: ns},
				Spec: clawv1.ClawAgentSpec{
					Workspace: clawv1.WorkspaceSpec{
						Mode:          clawv1.WorkspaceModePersistent,
						StorageSize:   "1Gi",
						ReclaimPolicy: clawv1.ReclaimPolicyDelete,
					},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).To(Succeed())
			reconcileAgent(agentName)
		})

		AfterEach(func() {
			deleteIfExists(&clawv1.ClawAgent{}, types.NamespacedName{Name: agentName, Namespace: ns})
			deleteIfExists(&corev1.PersistentVolumeClaim{}, types.NamespacedName{Name: agentName + clawv1.PVCSuffix, Namespace: ns})
		})

		It("should create PVC with owner reference pointing to the agent", func() {
			pvc := &corev1.PersistentVolumeClaim{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: agentName + clawv1.PVCSuffix, Namespace: ns}, pvc)).To(Succeed())
			Expect(pvc.OwnerReferences).NotTo(BeEmpty(), "delete PVC must have owner reference")
			Expect(pvc.OwnerReferences[0].Name).To(Equal(agentName))
		})
	})

	// -----------------------------------------------------------------------
	// Test: Credential security — no EnvFrom, no apiKey
	// -----------------------------------------------------------------------
	Context("credential security — agent has no API key access", func() {
		const agentName = "test-no-secrets"

		BeforeEach(func() {
			agent := &clawv1.ClawAgent{
				ObjectMeta: metav1.ObjectMeta{Name: agentName, Namespace: ns},
				Spec:       clawv1.ClawAgentSpec{},
			}
			Expect(k8sClient.Create(ctx, agent)).To(Succeed())
			reconcileAgent(agentName)
		})

		AfterEach(func() {
			deleteIfExists(&clawv1.ClawAgent{}, types.NamespacedName{Name: agentName, Namespace: ns})
		})

		It("should have NO EnvFrom on the openclaw container", func() {
			dep := getDeployment(agentName)
			mc := mainContainer(dep)
			Expect(mc.EnvFrom).To(BeEmpty(), "agent container must not have EnvFrom — no secret injection")
		})

		It("should NOT have real API key in openclaw.json", func() {
			cm := getConfigMap(agentName + "-openclaw-config")
			openclawJSON := cm.Data["openclaw.json"]
			Expect(openclawJSON).NotTo(BeEmpty())
			// Must never contain the real key or env var reference
			Expect(openclawJSON).NotTo(ContainSubstring("ANTHROPIC_API_KEY"))
		})

		It("should NOT have any credentials volume when credentialsSecret is empty", func() {
			dep := getDeployment(agentName)
			vol := findVolume(dep, "credentials-secret")
			Expect(vol).To(BeNil(), "no credentials volume when credentialsSecret is not set")
		})
	})

	// -----------------------------------------------------------------------
	// Test: Credentials secret mounting
	// -----------------------------------------------------------------------
	Context("with credentialsSecret configured", func() {
		const agentName = "test-with-creds"
		const secretName = "my-agent-creds"

		BeforeEach(func() {
			agent := &clawv1.ClawAgent{
				ObjectMeta: metav1.ObjectMeta{Name: agentName, Namespace: ns},
				Spec: clawv1.ClawAgentSpec{
					CredentialsSecret: secretName,
				},
			}
			Expect(k8sClient.Create(ctx, agent)).To(Succeed())
			reconcileAgent(agentName)
		})

		AfterEach(func() {
			deleteIfExists(&clawv1.ClawAgent{}, types.NamespacedName{Name: agentName, Namespace: ns})
		})

		It("should mount the credentials secret as a volume", func() {
			dep := getDeployment(agentName)
			vol := findVolume(dep, "credentials-secret")
			Expect(vol).NotTo(BeNil(), "credentials-secret volume must exist")
			Expect(vol.VolumeSource.Secret).NotTo(BeNil())
			Expect(vol.VolumeSource.Secret.SecretName).To(Equal(secretName))
			Expect(*vol.VolumeSource.Secret.DefaultMode).To(Equal(int32(0400)))
			Expect(*vol.VolumeSource.Secret.Optional).To(BeTrue())
		})

		It("should mount credentials read-only at the correct path", func() {
			dep := getDeployment(agentName)
			mc := mainContainer(dep)
			mount := findVolumeMount(mc, "credentials-secret")
			Expect(mount).NotTo(BeNil(), "credentials-secret mount must exist")
			Expect(mount.MountPath).To(Equal("/home/node/.openclaw/credentials"))
			Expect(mount.ReadOnly).To(BeTrue())
		})

		It("should auto-inject credential deny patterns in observeclaw config", func() {
			cm := getConfigMap(agentName + "-openclaw-config")
			openclawJSON := cm.Data["openclaw.json"]
			Expect(openclawJSON).NotTo(BeEmpty())

			// Parse the full config to dig into the observeclaw plugin config.
			var cfg map[string]any
			Expect(json.Unmarshal([]byte(openclawJSON), &cfg)).To(Succeed())

			plugins, ok := cfg["plugins"].(map[string]any)
			Expect(ok).To(BeTrue())
			entries, ok := plugins["entries"].(map[string]any)
			Expect(ok).To(BeTrue())
			observeclaw, ok := entries["observeclaw"].(map[string]any)
			Expect(ok).To(BeTrue())
			config, ok := observeclaw["config"].(map[string]any)
			Expect(ok).To(BeTrue())
			toolPolicy, ok := config["toolPolicy"].(map[string]any)
			Expect(ok).To(BeTrue())
			defaults, ok := toolPolicy["defaults"].(map[string]any)
			Expect(ok).To(BeTrue())

			// deny should be a list containing credential protection patterns.
			denyRaw, ok := defaults["deny"].([]any)
			Expect(ok).To(BeTrue())
			var denyList []string
			for _, d := range denyRaw {
				denyList = append(denyList, d.(string))
			}

			Expect(denyList).To(ContainElement("/home/node/.openclaw/credentials/*"))
			Expect(denyList).To(ContainElement("cat.*credentials"))
			Expect(denyList).To(ContainElement("grep.*credentials"))
			Expect(denyList).To(ContainElement("base64.*credentials"))
		})
	})

	// -----------------------------------------------------------------------
	// Test: No credential deny patterns when no secret configured
	// -----------------------------------------------------------------------
	Context("without credentialsSecret — no deny patterns injected", func() {
		const agentName = "test-no-cred-deny"

		BeforeEach(func() {
			agent := &clawv1.ClawAgent{
				ObjectMeta: metav1.ObjectMeta{Name: agentName, Namespace: ns},
				Spec:       clawv1.ClawAgentSpec{},
			}
			Expect(k8sClient.Create(ctx, agent)).To(Succeed())
			reconcileAgent(agentName)
		})

		AfterEach(func() {
			deleteIfExists(&clawv1.ClawAgent{}, types.NamespacedName{Name: agentName, Namespace: ns})
		})

		It("should NOT have credential deny patterns in observeclaw config", func() {
			cm := getConfigMap(agentName + "-openclaw-config")
			openclawJSON := cm.Data["openclaw.json"]
			Expect(openclawJSON).NotTo(ContainSubstring("/home/node/.openclaw/credentials/*"))
		})
	})

	// -----------------------------------------------------------------------
	// Test: Combined — persistent + credentials
	// -----------------------------------------------------------------------
	Context("persistent workspace + credentials secret together", func() {
		const agentName = "test-combined"
		const secretName = "combined-creds"

		BeforeEach(func() {
			agent := &clawv1.ClawAgent{
				ObjectMeta: metav1.ObjectMeta{Name: agentName, Namespace: ns},
				Spec: clawv1.ClawAgentSpec{
					Workspace: clawv1.WorkspaceSpec{
						Mode:        clawv1.WorkspaceModePersistent,
						StorageSize: "20Gi",
					},
					CredentialsSecret: secretName,
				},
			}
			Expect(k8sClient.Create(ctx, agent)).To(Succeed())
			reconcileAgent(agentName)
		})

		AfterEach(func() {
			deleteIfExists(&clawv1.ClawAgent{}, types.NamespacedName{Name: agentName, Namespace: ns})
			deleteIfExists(&corev1.PersistentVolumeClaim{}, types.NamespacedName{Name: agentName + clawv1.PVCSuffix, Namespace: ns})
		})

		It("should have both PVC volume and credentials secret volume", func() {
			dep := getDeployment(agentName)

			// PVC volume
			homeVol := findVolume(dep, "openclaw-home")
			Expect(homeVol).NotTo(BeNil())
			Expect(homeVol.VolumeSource.PersistentVolumeClaim).NotTo(BeNil())
			Expect(homeVol.VolumeSource.PersistentVolumeClaim.ClaimName).To(Equal(agentName + clawv1.PVCSuffix))

			// Credentials volume
			credVol := findVolume(dep, "credentials-secret")
			Expect(credVol).NotTo(BeNil())
			Expect(credVol.VolumeSource.Secret.SecretName).To(Equal(secretName))
		})

		It("should have PVC with 20Gi", func() {
			pvc := &corev1.PersistentVolumeClaim{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: agentName + clawv1.PVCSuffix, Namespace: ns}, pvc)).To(Succeed())
			storageReq := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
			Expect(storageReq.String()).To(Equal("20Gi"))
		})

		It("should still have zero API key exposure", func() {
			dep := getDeployment(agentName)
			mc := mainContainer(dep)
			Expect(mc.EnvFrom).To(BeEmpty())

			// No ANTHROPIC_API_KEY in env vars either
			for _, env := range mc.Env {
				Expect(strings.ToUpper(env.Name)).NotTo(ContainSubstring("API_KEY"))
			}

			cm := getConfigMap(agentName + "-openclaw-config")
			Expect(cm.Data["openclaw.json"]).NotTo(ContainSubstring("ANTHROPIC_API_KEY"))
		})
	})

	// -----------------------------------------------------------------------
	// Test: Gateway config — openclaw.json provider has no apiKey even
	// when a gateway is referenced
	// -----------------------------------------------------------------------
	Context("with gateway referenced — provider config has no apiKey", func() {
		const agentName = "test-gateway-nokey"
		const gwName = "test-gw"

		BeforeEach(func() {
			// Create a ClawGateway so the agent controller can resolve it.
			gw := &clawv1.ClawGateway{
				ObjectMeta: metav1.ObjectMeta{Name: gwName, Namespace: ns},
				Spec: clawv1.ClawGatewaySpec{
					Topology: "centralized",
					Port:     8443,
				},
			}
			Expect(k8sClient.Create(ctx, gw)).To(Succeed())

			agent := &clawv1.ClawAgent{
				ObjectMeta: metav1.ObjectMeta{Name: agentName, Namespace: ns},
				Spec: clawv1.ClawAgentSpec{
					Gateway: gwName,
				},
			}
			Expect(k8sClient.Create(ctx, agent)).To(Succeed())
			reconcileAgent(agentName)
		})

		AfterEach(func() {
			deleteIfExists(&clawv1.ClawAgent{}, types.NamespacedName{Name: agentName, Namespace: ns})
			deleteIfExists(&clawv1.ClawGateway{}, types.NamespacedName{Name: gwName, Namespace: ns})
		})

		It("should reference gateway URL but have NO apiKey in provider config", func() {
			cm := getConfigMap(agentName + "-openclaw-config")
			openclawJSON := cm.Data["openclaw.json"]

			// Should have the gateway URL
			Expect(openclawJSON).To(ContainSubstring("gateway"))
			Expect(openclawJSON).To(ContainSubstring("gateway-anthropic"))

			// Must never contain the real key or env var reference
			Expect(openclawJSON).NotTo(ContainSubstring("ANTHROPIC_API_KEY"))

			// Parse to verify the provider uses a sentinel, not a real key
			var cfg map[string]any
			Expect(json.Unmarshal([]byte(openclawJSON), &cfg)).To(Succeed())
			models, ok := cfg["models"].(map[string]any)
			Expect(ok).To(BeTrue(), "models section should exist when gateway is referenced")
			providers, ok := models["providers"].(map[string]any)
			Expect(ok).To(BeTrue())
			gwProvider, ok := providers["gateway-anthropic"].(map[string]any)
			Expect(ok).To(BeTrue())
			apiKey, hasApiKey := gwProvider["apiKey"]
			Expect(hasApiKey).To(BeTrue(), "gateway-anthropic provider should have apiKey (sentinel)")
			Expect(apiKey).To(Equal("gateway-managed"), "apiKey must be the sentinel value, not a real key")
		})
	})

	// -----------------------------------------------------------------------
	// Test: Channel resolution and config generation
	// -----------------------------------------------------------------------
	Context("with ClawChannel referenced", func() {
		const agentName = "test-channel-agent"
		const channelName = "test-telegram"

		BeforeEach(func() {
			enabled := true
			channel := &clawv1.ClawChannel{
				ObjectMeta: metav1.ObjectMeta{Name: channelName, Namespace: ns},
				Spec: clawv1.ClawChannelSpec{
					Type:              clawv1.ChannelTypeTelegram,
					Enabled:           &enabled,
					CredentialsSecret: "tg-creds",
					Config: map[string]string{
						"dmPolicy":    "pairing",
						"groupPolicy": "allowlist",
					},
				},
			}
			Expect(k8sClient.Create(ctx, channel)).To(Succeed())

			agent := &clawv1.ClawAgent{
				ObjectMeta: metav1.ObjectMeta{Name: agentName, Namespace: ns},
				Spec: clawv1.ClawAgentSpec{
					Channels: []string{channelName},
					Model: clawv1.AgentModelSpec{
						Provider: "openai",
						Name:     "gpt-4.1",
					},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).To(Succeed())
			reconcileAgent(agentName)
		})

		AfterEach(func() {
			deleteIfExists(&clawv1.ClawAgent{}, types.NamespacedName{Name: agentName, Namespace: ns})
			deleteIfExists(&clawv1.ClawChannel{}, types.NamespacedName{Name: channelName, Namespace: ns})
		})

		It("should generate channels config in openclaw.json with ${VAR} placeholders", func() {
			cm := getConfigMap(agentName + "-openclaw-config")
			openclawJSON := cm.Data["openclaw.json"]

			var cfg map[string]any
			Expect(json.Unmarshal([]byte(openclawJSON), &cfg)).To(Succeed())

			channels, ok := cfg["channels"].(map[string]any)
			Expect(ok).To(BeTrue(), "channels section must exist")
			tg, ok := channels["telegram"].(map[string]any)
			Expect(ok).To(BeTrue(), "telegram channel must exist")
			Expect(tg["enabled"]).To(Equal(true))
			Expect(tg["botToken"]).To(Equal("${TELEGRAM_BOT_TOKEN}"))
			Expect(tg["dmPolicy"]).To(Equal("pairing"))
			Expect(tg["groupPolicy"]).To(Equal("allowlist"))
		})

		It("should auto-enable the telegram plugin", func() {
			cm := getConfigMap(agentName + "-openclaw-config")
			var cfg map[string]any
			Expect(json.Unmarshal([]byte(cm.Data["openclaw.json"]), &cfg)).To(Succeed())

			plugins := cfg["plugins"].(map[string]any)["entries"].(map[string]any)
			tgPlugin, ok := plugins["telegram"].(map[string]any)
			Expect(ok).To(BeTrue(), "telegram plugin must be auto-enabled")
			Expect(tgPlugin["enabled"]).To(Equal(true))
		})

		It("should inject channel credential secret as EnvFrom", func() {
			dep := getDeployment(agentName)
			mc := mainContainer(dep)
			found := false
			for _, ef := range mc.EnvFrom {
				if ef.SecretRef != nil && ef.SecretRef.Name == "tg-creds" {
					found = true
					break
				}
			}
			Expect(found).To(BeTrue(), "tg-creds secret must be injected as EnvFrom")
		})

		It("should not have raw tokens in the ConfigMap", func() {
			cm := getConfigMap(agentName + "-openclaw-config")
			// The ConfigMap should only have ${VAR} placeholders, never real values
			Expect(cm.Data["openclaw.json"]).NotTo(MatchRegexp(`\d{10}:`))
		})

		It("should set agents.defaults.model.primary from spec", func() {
			cm := getConfigMap(agentName + "-openclaw-config")
			var cfg map[string]any
			Expect(json.Unmarshal([]byte(cm.Data["openclaw.json"]), &cfg)).To(Succeed())

			defaults := cfg["agents"].(map[string]any)["defaults"].(map[string]any)
			model := defaults["model"].(map[string]any)
			Expect(model["primary"]).To(Equal("openai/gpt-4.1"))
		})

		It("should register the openai provider with ${OPENAI_API_KEY}", func() {
			cm := getConfigMap(agentName + "-openclaw-config")
			var cfg map[string]any
			Expect(json.Unmarshal([]byte(cm.Data["openclaw.json"]), &cfg)).To(Succeed())

			providers := cfg["models"].(map[string]any)["providers"].(map[string]any)
			openai, ok := providers["openai"].(map[string]any)
			Expect(ok).To(BeTrue(), "openai provider must be registered")
			Expect(openai["apiKey"]).To(Equal("${OPENAI_API_KEY}"))
			Expect(openai["api"]).To(Equal("openai-responses"))
		})
	})

	// -----------------------------------------------------------------------
	// Test: No model specified — no panic, no model section
	// -----------------------------------------------------------------------
	Context("with no model specified", func() {
		const agentName = "test-no-model"

		BeforeEach(func() {
			agent := &clawv1.ClawAgent{
				ObjectMeta: metav1.ObjectMeta{Name: agentName, Namespace: ns},
				Spec:       clawv1.ClawAgentSpec{},
			}
			Expect(k8sClient.Create(ctx, agent)).To(Succeed())
			reconcileAgent(agentName)
		})

		AfterEach(func() {
			deleteIfExists(&clawv1.ClawAgent{}, types.NamespacedName{Name: agentName, Namespace: ns})
		})

		It("should not set model.primary in agents.defaults", func() {
			cm := getConfigMap(agentName + "-openclaw-config")
			var cfg map[string]any
			Expect(json.Unmarshal([]byte(cm.Data["openclaw.json"]), &cfg)).To(Succeed())

			defaults := cfg["agents"].(map[string]any)["defaults"].(map[string]any)
			_, hasModel := defaults["model"]
			Expect(hasModel).To(BeFalse(), "no model section when provider is empty")
		})

		It("should not inject openclaw-api-keys EnvFrom", func() {
			dep := getDeployment(agentName)
			mc := mainContainer(dep)
			for _, ef := range mc.EnvFrom {
				if ef.SecretRef != nil {
					Expect(ef.SecretRef.Name).NotTo(Equal(clawv1.DefaultProviderAPIKeysSecret))
				}
			}
		})
	})
})
