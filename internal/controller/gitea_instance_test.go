package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	hyperv1 "hyperspike.io/gitea-operator/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestExternalInstanceOnlyEstablishesConnection(t *testing.T) {
	const token = "external-admin-token"
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/version" {
			http.NotFound(response, request)
			return
		}
		if request.Header.Get("Authorization") != "token "+token {
			http.Error(response, "unauthorized", http.StatusUnauthorized)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"version":"16.0.2"}`))
	}))
	defer server.Close()

	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, appsv1.AddToScheme, hyperv1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	instance := &hyperv1.Gitea{
		ObjectMeta: metav1.ObjectMeta{Name: "forgejo", Namespace: "forgejo"},
		Spec: hyperv1.GiteaSpec{
			ManagementPolicy: hyperv1.ManagementPolicyExternal,
			Connection: hyperv1.ConnectionSpec{
				URL: server.URL,
				AdminTokenSecretRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "forgejo-api"},
					Key:                  "token",
				},
			},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "forgejo-api", Namespace: "forgejo"},
		Data:       map[string][]byte{"token": []byte(token)},
	}
	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&hyperv1.Gitea{}).
		WithObjects(instance, secret).
		Build()
	reconciler := &GiteaReconciler{
		Client:   kubeClient,
		Scheme:   scheme,
		Recorder: events.NewFakeRecorder(10),
	}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{
		Name: instance.Name, Namespace: instance.Namespace,
	}}); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	updated := &hyperv1.Gitea{}
	if err := kubeClient.Get(context.Background(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, updated); err != nil {
		t.Fatal(err)
	}
	if !updated.Status.Ready || updated.Status.Endpoint != server.URL {
		t.Fatalf("unexpected status: %#v", updated.Status)
	}

	statefulSets := &appsv1.StatefulSetList{}
	if err := kubeClient.List(context.Background(), statefulSets); err != nil {
		t.Fatal(err)
	}
	if len(statefulSets.Items) != 0 {
		t.Fatalf("external instance created %d StatefulSets", len(statefulSets.Items))
	}
	services := &corev1.ServiceList{}
	if err := kubeClient.List(context.Background(), services); err != nil {
		t.Fatal(err)
	}
	if len(services.Items) != 0 {
		t.Fatalf("external instance created %d Services", len(services.Items))
	}
}

func TestExistingDatabaseNameFromSecret(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "forgejo-pg", Namespace: "forgejo"},
		Data:       map[string][]byte{"database": []byte("forgejo")},
	}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
	reconciler := &GiteaReconciler{Client: kubeClient}
	instance := &hyperv1.Gitea{
		ObjectMeta: metav1.ObjectMeta{Name: "forgejo", Namespace: "forgejo"},
		Spec: hyperv1.GiteaSpec{Postgres: hyperv1.PostgresSpec{Existing: &hyperv1.ExistingPostgresSpec{
			DatabaseSecretRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: secret.Name},
				Key:                  "database",
			},
		}}},
	}

	got, err := reconciler.existingDatabaseName(context.Background(), instance)
	if err != nil {
		t.Fatalf("existingDatabaseName() error = %v", err)
	}
	if got != "forgejo" {
		t.Fatalf("existingDatabaseName() = %q, want forgejo", got)
	}
}

func TestManagedStatefulSetReusesPVCAndReconcilesImage(t *testing.T) {
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, appsv1.AddToScheme, hyperv1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	reconciler := &GiteaReconciler{
		Client:   kubeClient,
		Scheme:   scheme,
		Recorder: events.NewFakeRecorder(10),
	}
	instance := &hyperv1.Gitea{
		TypeMeta:   metav1.TypeMeta{APIVersion: hyperv1.SchemeGroupVersion.String(), Kind: "Gitea"},
		ObjectMeta: metav1.ObjectMeta{Name: "forgejo", Namespace: "forgejo", UID: "instance-uid"},
		Spec: hyperv1.GiteaSpec{
			Image:          "codeberg.org/forgejo/forgejo:16.0.2-rootless",
			Rootless:       true,
			DeletionPolicy: hyperv1.DeletionPolicyRetain,
			Postgres: hyperv1.PostgresSpec{Existing: &hyperv1.ExistingPostgresSpec{
				Host:                 "pg-rw.database.svc",
				Database:             "forgejo",
				CredentialsSecretRef: corev1.LocalObjectReference{Name: "forgejo-pg"},
			}},
			Persistence: hyperv1.PersistenceSpec{Data: hyperv1.VolumeSpec{
				ExistingClaim: "forgejo-data",
				MountPath:     "/data",
			}},
			Workload: hyperv1.WorkloadSpec{
				Name: "forgejo-managed",
				Service: hyperv1.ManagedServiceSpec{
					Name: "forgejo-managed",
				},
			},
		},
	}

	if err := reconciler.upsertGiteaSts(context.Background(), instance); err != nil {
		t.Fatalf("upsertGiteaSts() create error = %v", err)
	}
	key := types.NamespacedName{Name: "forgejo-managed", Namespace: "forgejo"}
	statefulSet := &appsv1.StatefulSet{}
	if err := kubeClient.Get(context.Background(), key, statefulSet); err != nil {
		t.Fatal(err)
	}
	if statefulSet.Spec.ServiceName != "forgejo-managed" {
		t.Fatalf("serviceName = %q", statefulSet.Spec.ServiceName)
	}
	if len(statefulSet.Spec.VolumeClaimTemplates) != 0 {
		t.Fatal("operator created a volume claim template while existingClaim is set")
	}
	if statefulSet.Spec.PersistentVolumeClaimRetentionPolicy == nil ||
		statefulSet.Spec.PersistentVolumeClaimRetentionPolicy.WhenDeleted != appsv1.RetainPersistentVolumeClaimRetentionPolicyType {
		t.Fatal("PVC retention policy is not Retain")
	}
	if statefulSet.Spec.Template.Spec.Containers[0].Name != Forgejo {
		t.Fatalf("main container = %q, want %q", statefulSet.Spec.Template.Spec.Containers[0].Name, Forgejo)
	}
	if len(statefulSet.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("default managed workload has %d containers, want only Forgejo", len(statefulSet.Spec.Template.Spec.Containers))
	}

	instance.Spec.Image = "codeberg.org/forgejo/forgejo:16.0.3-rootless"
	if err := reconciler.upsertGiteaSts(context.Background(), instance); err != nil {
		t.Fatalf("upsertGiteaSts() update error = %v", err)
	}
	if err := kubeClient.Get(context.Background(), key, statefulSet); err != nil {
		t.Fatal(err)
	}
	if got := statefulSet.Spec.Template.Spec.Containers[0].Image; got != instance.Spec.Image {
		t.Fatalf("image = %q, want %q", got, instance.Spec.Image)
	}
}
