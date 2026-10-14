package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	hyperv1 "hyperspike.io/gitea-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const externalAdminToken = "external-admin-token"

func TestOrgRetainDeletionDoesNotRequireInstance(t *testing.T) {
	for _, policy := range []hyperv1.DeletionPolicy{"", hyperv1.DeletionPolicyRetain} {
		name := string(policy)
		if name == "" {
			name = "default"
		}
		t.Run(name, func(t *testing.T) {
			scheme := forgejoResourceScheme(t)
			now := metav1.Now()
			org := &hyperv1.Org{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "platform",
					Namespace:         "forgejo",
					Finalizers:        []string{orgFinalizer},
					DeletionTimestamp: &now,
				},
				Spec: hyperv1.OrgSpec{
					DeletionPolicy: policy,
					Instance:       hyperv1.InstanceType{Name: "missing-instance"},
				},
			}
			kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(org).Build()
			reconciler := &OrgReconciler{Client: kubeClient, Scheme: scheme}

			if _, err := reconciler.Reconcile(context.Background(), resourceRequest(org)); err != nil {
				t.Fatalf("Reconcile() error = %v", err)
			}
			assertReleasedFinalizer(t, kubeClient, org, &hyperv1.Org{}, orgFinalizer)
		})
	}
}

func TestRepoRetainDeletionDoesNotRequireOrg(t *testing.T) {
	for _, policy := range []hyperv1.DeletionPolicy{"", hyperv1.DeletionPolicyRetain} {
		name := string(policy)
		if name == "" {
			name = "default"
		}
		t.Run(name, func(t *testing.T) {
			scheme := forgejoResourceScheme(t)
			now := metav1.Now()
			repo := &hyperv1.Repo{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "control-plane",
					Namespace:         "forgejo",
					Finalizers:        []string{repoFinalizer},
					DeletionTimestamp: &now,
				},
				Spec: hyperv1.RepoSpec{
					DeletionPolicy: policy,
					Org:            &hyperv1.OrgRef{Name: "missing-org"},
				},
			}
			kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(repo).Build()
			reconciler := &RepoReconciler{Client: kubeClient, Scheme: scheme}

			if _, err := reconciler.Reconcile(context.Background(), resourceRequest(repo)); err != nil {
				t.Fatalf("Reconcile() error = %v", err)
			}
			assertReleasedFinalizer(t, kubeClient, repo, &hyperv1.Repo{}, repoFinalizer)
		})
	}
}

func TestRepoDeletePolicyDeletesRemoteRepository(t *testing.T) {
	var deletes atomic.Int32
	server := httptest.NewServer(authenticatedForgejoHandler(t, func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodDelete && request.URL.Path == "/api/v1/repos/platform/control-plane" {
			deletes.Add(1)
			response.WriteHeader(http.StatusNoContent)
			return
		}
		t.Errorf("unexpected Forgejo request: %s %s", request.Method, request.URL.String())
		http.NotFound(response, request)
	}))
	defer server.Close()

	scheme := forgejoResourceScheme(t)
	instance, secret := externalForgejoObjects(server.URL)
	org := &hyperv1.Org{
		ObjectMeta: metav1.ObjectMeta{Name: "platform", Namespace: "forgejo"},
		Spec: hyperv1.OrgSpec{
			Instance: hyperv1.InstanceType{Name: instance.Name},
		},
	}
	now := metav1.Now()
	repo := &hyperv1.Repo{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "control-plane",
			Namespace:         "forgejo",
			Finalizers:        []string{repoFinalizer},
			DeletionTimestamp: &now,
		},
		Spec: hyperv1.RepoSpec{
			DeletionPolicy: hyperv1.DeletionPolicyDelete,
			Org:            &hyperv1.OrgRef{Name: org.Name},
		},
	}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, secret, org, repo).Build()
	reconciler := &RepoReconciler{Client: kubeClient, Scheme: scheme}

	if _, err := reconciler.Reconcile(context.Background(), resourceRequest(repo)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if got := deletes.Load(); got != 1 {
		t.Fatalf("repository delete requests = %d, want 1", got)
	}
	assertReleasedFinalizer(t, kubeClient, repo, &hyperv1.Repo{}, repoFinalizer)
}

func TestOrgDeletePolicyTreatsMissingRemoteOrganizationAsDeleted(t *testing.T) {
	var deletes atomic.Int32
	server := httptest.NewServer(authenticatedForgejoHandler(t, func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodDelete && request.URL.Path == "/api/v1/orgs/platform" {
			deletes.Add(1)
			http.NotFound(response, request)
			return
		}
		t.Errorf("unexpected Forgejo request: %s %s", request.Method, request.URL.String())
		http.NotFound(response, request)
	}))
	defer server.Close()

	scheme := forgejoResourceScheme(t)
	instance, secret := externalForgejoObjects(server.URL)
	now := metav1.Now()
	org := &hyperv1.Org{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "platform",
			Namespace:         "forgejo",
			Finalizers:        []string{orgFinalizer},
			DeletionTimestamp: &now,
		},
		Spec: hyperv1.OrgSpec{
			DeletionPolicy: hyperv1.DeletionPolicyDelete,
			Instance:       hyperv1.InstanceType{Name: instance.Name},
		},
	}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, secret, org).Build()
	reconciler := &OrgReconciler{Client: kubeClient, Scheme: scheme}

	if _, err := reconciler.Reconcile(context.Background(), resourceRequest(org)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if got := deletes.Load(); got != 1 {
		t.Fatalf("organization delete requests = %d, want 1", got)
	}
	assertReleasedFinalizer(t, kubeClient, org, &hyperv1.Org{}, orgFinalizer)
}

func TestOrgAdoptsExistingOrganizationOnExternalInstance(t *testing.T) {
	server := httptest.NewServer(authenticatedForgejoHandler(t, func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/orgs/platform":
			writeJSON(response, http.StatusOK, `{"username":"platform","full_name":"Platform","description":"Managed by Runik","visibility":"private"}`)
		case "/api/v1/orgs/platform/teams/search":
			writeJSON(response, http.StatusOK, `{"ok":true,"data":[]}`)
		default:
			t.Errorf("unexpected Forgejo request: %s %s", request.Method, request.URL.String())
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	scheme := forgejoResourceScheme(t)
	instance, secret := externalForgejoObjects(server.URL)
	org := &hyperv1.Org{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "platform",
			Namespace:  "forgejo",
			Finalizers: []string{orgFinalizer},
		},
		Spec: hyperv1.OrgSpec{
			FullName:    "Platform",
			Description: "Managed by Runik",
			Visibility:  "private",
			Instance:    hyperv1.InstanceType{Name: instance.Name},
		},
	}
	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&hyperv1.Gitea{}, &hyperv1.Org{}).
		WithObjects(instance, secret, org).
		Build()
	reconciler := &OrgReconciler{Client: kubeClient, Scheme: scheme}

	if _, err := reconciler.Reconcile(context.Background(), resourceRequest(org)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	updated := &hyperv1.Org{}
	if err := kubeClient.Get(context.Background(), client.ObjectKeyFromObject(org), updated); err != nil {
		t.Fatal(err)
	}
	if !updated.Status.Provisioned {
		t.Fatal("existing organization was not marked provisioned")
	}
}

func TestRepoCreatesOnceAndThenAdoptsOnExternalInstance(t *testing.T) {
	var exists atomic.Bool
	var creates atomic.Int32
	server := httptest.NewServer(authenticatedForgejoHandler(t, func(response http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/repos/platform/control-plane":
			if !exists.Load() {
				http.NotFound(response, request)
				return
			}
			writeJSON(response, http.StatusOK, `{"name":"control-plane","full_name":"platform/control-plane","description":"Managed by Runik","private":true,"owner":{"login":"platform"}}`)
		case request.Method == http.MethodPost && request.URL.Path == "/api/v1/orgs/platform/repos":
			creates.Add(1)
			exists.Store(true)
			writeJSON(response, http.StatusCreated, `{"name":"control-plane","full_name":"platform/control-plane","description":"Managed by Runik","private":true,"owner":{"login":"platform"}}`)
		default:
			t.Errorf("unexpected Forgejo request: %s %s", request.Method, request.URL.String())
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	scheme := forgejoResourceScheme(t)
	instance, secret := externalForgejoObjects(server.URL)
	org := &hyperv1.Org{
		ObjectMeta: metav1.ObjectMeta{Name: "platform", Namespace: "forgejo"},
		Spec: hyperv1.OrgSpec{
			Instance: hyperv1.InstanceType{Name: instance.Name},
		},
	}
	repo := &hyperv1.Repo{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "control-plane",
			Namespace:  "forgejo",
			Finalizers: []string{repoFinalizer},
		},
		Spec: hyperv1.RepoSpec{
			Description: "Managed by Runik",
			Private:     true,
			Org:         &hyperv1.OrgRef{Name: org.Name},
		},
	}
	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&hyperv1.Gitea{}, &hyperv1.Org{}, &hyperv1.Repo{}).
		WithObjects(instance, secret, org, repo).
		Build()
	reconciler := &RepoReconciler{Client: kubeClient, Scheme: scheme}

	for reconcile := 1; reconcile <= 2; reconcile++ {
		if _, err := reconciler.Reconcile(context.Background(), resourceRequest(repo)); err != nil {
			t.Fatalf("Reconcile() #%d error = %v", reconcile, err)
		}
	}
	if got := creates.Load(); got != 1 {
		t.Fatalf("repository create requests = %d, want 1", got)
	}
	updated := &hyperv1.Repo{}
	if err := kubeClient.Get(context.Background(), client.ObjectKeyFromObject(repo), updated); err != nil {
		t.Fatal(err)
	}
	if !updated.Status.Provisioned {
		t.Fatal("repository was not marked provisioned")
	}
}

func authenticatedForgejoHandler(t *testing.T, next http.HandlerFunc) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "token "+externalAdminToken {
			http.Error(response, "unauthorized", http.StatusUnauthorized)
			return
		}
		if request.URL.Path == "/api/v1/version" {
			writeJSON(response, http.StatusOK, `{"version":"16.0.2"}`)
			return
		}
		next(response, request)
	})
}

func forgejoResourceScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, hyperv1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	return scheme
}

func externalForgejoObjects(endpoint string) (*hyperv1.Gitea, *corev1.Secret) {
	instance := &hyperv1.Gitea{
		ObjectMeta: metav1.ObjectMeta{Name: "forgejo", Namespace: "forgejo"},
		Spec: hyperv1.GiteaSpec{
			ManagementPolicy: hyperv1.ManagementPolicyExternal,
			Connection: hyperv1.ConnectionSpec{
				URL: endpoint,
				AdminTokenSecretRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "forgejo-api"},
					Key:                  "token",
				},
			},
		},
		Status: hyperv1.GiteaStatus{Ready: true, Endpoint: endpoint},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "forgejo-api", Namespace: "forgejo"},
		Data:       map[string][]byte{"token": []byte(externalAdminToken)},
	}
	return instance, secret
}

func resourceRequest(object client.Object) ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Name: object.GetName(), Namespace: object.GetNamespace()}}
}

func assertReleasedFinalizer(t *testing.T, kubeClient client.Client, source, fetched client.Object, finalizer string) {
	t.Helper()
	err := kubeClient.Get(context.Background(), client.ObjectKeyFromObject(source), fetched)
	if apierrors.IsNotFound(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if controllerutil.ContainsFinalizer(fetched, finalizer) {
		t.Fatalf("finalizer %q was not released", finalizer)
	}
}

func writeJSON(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_, _ = response.Write([]byte(body))
}
