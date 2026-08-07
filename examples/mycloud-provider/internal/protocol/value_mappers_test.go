package protocol

import (
	"reflect"
	"testing"
)
import provider "github.com/mycloud/terraform-provider-mycloud/internal/provider"
// TestConfigModelType verifies that the generated ConfigModelType function returns an object type.
func TestConfigModelType(t *testing.T) {
	typ := ConfigModelType()
	if typ == nil {
		t.Fatal("Type() returned nil")
	}
}
// TestConfigModelRoundTrip verifies that ConfigModelToValue and ConfigModelFromValue are inverses for an empty model.
func TestConfigModelRoundTrip(t *testing.T) {
	original := provider.ConfigModel{}
	v, err := ConfigModelToValue(original)
	if err != nil {
		t.Fatalf("ToValue error: %v", err)
	}
	got, err := ConfigModelFromValue(v)
	if err != nil {
		t.Fatalf("FromValue error: %v", err)
	}
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("round-trip mismatch:\n got: %+v\nwant: %+v", got, original)
	}
}
// TestInstanceModelType verifies that the generated InstanceModelType function returns an object type.
func TestInstanceModelType(t *testing.T) {
	typ := InstanceModelType()
	if typ == nil {
		t.Fatal("Type() returned nil")
	}
}
// TestInstanceModelRoundTrip verifies that InstanceModelToValue and InstanceModelFromValue are inverses for an empty model.
func TestInstanceModelRoundTrip(t *testing.T) {
	original := provider.InstanceModel{}
	v, err := InstanceModelToValue(original)
	if err != nil {
		t.Fatalf("ToValue error: %v", err)
	}
	got, err := InstanceModelFromValue(v)
	if err != nil {
		t.Fatalf("FromValue error: %v", err)
	}
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("round-trip mismatch:\n got: %+v\nwant: %+v", got, original)
	}
}
// TestInstanceModelSpecType verifies that the generated InstanceModelSpecType function returns an object type.
func TestInstanceModelSpecType(t *testing.T) {
	typ := InstanceModelSpecType()
	if typ == nil {
		t.Fatal("Type() returned nil")
	}
}
// TestInstanceModelSpecRoundTrip verifies that InstanceModelSpecToValue and InstanceModelSpecFromValue are inverses for an empty model.
func TestInstanceModelSpecRoundTrip(t *testing.T) {
	original := provider.InstanceModelSpec{}
	v, err := InstanceModelSpecToValue(original)
	if err != nil {
		t.Fatalf("ToValue error: %v", err)
	}
	got, err := InstanceModelSpecFromValue(v)
	if err != nil {
		t.Fatalf("FromValue error: %v", err)
	}
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("round-trip mismatch:\n got: %+v\nwant: %+v", got, original)
	}
}
// TestInstanceModelSpecContainersElemType verifies that the generated InstanceModelSpecContainersElemType function returns an object type.
func TestInstanceModelSpecContainersElemType(t *testing.T) {
	typ := InstanceModelSpecContainersElemType()
	if typ == nil {
		t.Fatal("Type() returned nil")
	}
}
// TestInstanceModelSpecContainersElemRoundTrip verifies that InstanceModelSpecContainersElemToValue and InstanceModelSpecContainersElemFromValue are inverses for an empty model.
func TestInstanceModelSpecContainersElemRoundTrip(t *testing.T) {
	original := provider.InstanceModelSpecContainersElem{}
	v, err := InstanceModelSpecContainersElemToValue(original)
	if err != nil {
		t.Fatalf("ToValue error: %v", err)
	}
	got, err := InstanceModelSpecContainersElemFromValue(v)
	if err != nil {
		t.Fatalf("FromValue error: %v", err)
	}
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("round-trip mismatch:\n got: %+v\nwant: %+v", got, original)
	}
}
// TestInstanceModelStatusType verifies that the generated InstanceModelStatusType function returns an object type.
func TestInstanceModelStatusType(t *testing.T) {
	typ := InstanceModelStatusType()
	if typ == nil {
		t.Fatal("Type() returned nil")
	}
}
// TestInstanceModelStatusRoundTrip verifies that InstanceModelStatusToValue and InstanceModelStatusFromValue are inverses for an empty model.
func TestInstanceModelStatusRoundTrip(t *testing.T) {
	original := provider.InstanceModelStatus{}
	v, err := InstanceModelStatusToValue(original)
	if err != nil {
		t.Fatalf("ToValue error: %v", err)
	}
	got, err := InstanceModelStatusFromValue(v)
	if err != nil {
		t.Fatalf("FromValue error: %v", err)
	}
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("round-trip mismatch:\n got: %+v\nwant: %+v", got, original)
	}
}
// TestNetworkModelType verifies that the generated NetworkModelType function returns an object type.
func TestNetworkModelType(t *testing.T) {
	typ := NetworkModelType()
	if typ == nil {
		t.Fatal("Type() returned nil")
	}
}
// TestNetworkModelRoundTrip verifies that NetworkModelToValue and NetworkModelFromValue are inverses for an empty model.
func TestNetworkModelRoundTrip(t *testing.T) {
	original := provider.NetworkModel{}
	v, err := NetworkModelToValue(original)
	if err != nil {
		t.Fatalf("ToValue error: %v", err)
	}
	got, err := NetworkModelFromValue(v)
	if err != nil {
		t.Fatalf("FromValue error: %v", err)
	}
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("round-trip mismatch:\n got: %+v\nwant: %+v", got, original)
	}
}
// TestNetworkModelSpecType verifies that the generated NetworkModelSpecType function returns an object type.
func TestNetworkModelSpecType(t *testing.T) {
	typ := NetworkModelSpecType()
	if typ == nil {
		t.Fatal("Type() returned nil")
	}
}
// TestNetworkModelSpecRoundTrip verifies that NetworkModelSpecToValue and NetworkModelSpecFromValue are inverses for an empty model.
func TestNetworkModelSpecRoundTrip(t *testing.T) {
	original := provider.NetworkModelSpec{}
	v, err := NetworkModelSpecToValue(original)
	if err != nil {
		t.Fatalf("ToValue error: %v", err)
	}
	got, err := NetworkModelSpecFromValue(v)
	if err != nil {
		t.Fatalf("FromValue error: %v", err)
	}
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("round-trip mismatch:\n got: %+v\nwant: %+v", got, original)
	}
}
// TestNetworkModelSpecPortsElemType verifies that the generated NetworkModelSpecPortsElemType function returns an object type.
func TestNetworkModelSpecPortsElemType(t *testing.T) {
	typ := NetworkModelSpecPortsElemType()
	if typ == nil {
		t.Fatal("Type() returned nil")
	}
}
// TestNetworkModelSpecPortsElemRoundTrip verifies that NetworkModelSpecPortsElemToValue and NetworkModelSpecPortsElemFromValue are inverses for an empty model.
func TestNetworkModelSpecPortsElemRoundTrip(t *testing.T) {
	original := provider.NetworkModelSpecPortsElem{}
	v, err := NetworkModelSpecPortsElemToValue(original)
	if err != nil {
		t.Fatalf("ToValue error: %v", err)
	}
	got, err := NetworkModelSpecPortsElemFromValue(v)
	if err != nil {
		t.Fatalf("FromValue error: %v", err)
	}
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("round-trip mismatch:\n got: %+v\nwant: %+v", got, original)
	}
}
// TestNetworkModelStatusType verifies that the generated NetworkModelStatusType function returns an object type.
func TestNetworkModelStatusType(t *testing.T) {
	typ := NetworkModelStatusType()
	if typ == nil {
		t.Fatal("Type() returned nil")
	}
}
// TestNetworkModelStatusRoundTrip verifies that NetworkModelStatusToValue and NetworkModelStatusFromValue are inverses for an empty model.
func TestNetworkModelStatusRoundTrip(t *testing.T) {
	original := provider.NetworkModelStatus{}
	v, err := NetworkModelStatusToValue(original)
	if err != nil {
		t.Fatalf("ToValue error: %v", err)
	}
	got, err := NetworkModelStatusFromValue(v)
	if err != nil {
		t.Fatalf("FromValue error: %v", err)
	}
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("round-trip mismatch:\n got: %+v\nwant: %+v", got, original)
	}
}
// TestProjectModelType verifies that the generated ProjectModelType function returns an object type.
func TestProjectModelType(t *testing.T) {
	typ := ProjectModelType()
	if typ == nil {
		t.Fatal("Type() returned nil")
	}
}
// TestProjectModelRoundTrip verifies that ProjectModelToValue and ProjectModelFromValue are inverses for an empty model.
func TestProjectModelRoundTrip(t *testing.T) {
	original := provider.ProjectModel{}
	v, err := ProjectModelToValue(original)
	if err != nil {
		t.Fatalf("ToValue error: %v", err)
	}
	got, err := ProjectModelFromValue(v)
	if err != nil {
		t.Fatalf("FromValue error: %v", err)
	}
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("round-trip mismatch:\n got: %+v\nwant: %+v", got, original)
	}
}
// TestSecretModelType verifies that the generated SecretModelType function returns an object type.
func TestSecretModelType(t *testing.T) {
	typ := SecretModelType()
	if typ == nil {
		t.Fatal("Type() returned nil")
	}
}
// TestSecretModelRoundTrip verifies that SecretModelToValue and SecretModelFromValue are inverses for an empty model.
func TestSecretModelRoundTrip(t *testing.T) {
	original := provider.SecretModel{}
	v, err := SecretModelToValue(original)
	if err != nil {
		t.Fatalf("ToValue error: %v", err)
	}
	got, err := SecretModelFromValue(v)
	if err != nil {
		t.Fatalf("FromValue error: %v", err)
	}
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("round-trip mismatch:\n got: %+v\nwant: %+v", got, original)
	}
}
// TestStackModelType verifies that the generated StackModelType function returns an object type.
func TestStackModelType(t *testing.T) {
	typ := StackModelType()
	if typ == nil {
		t.Fatal("Type() returned nil")
	}
}
// TestStackModelRoundTrip verifies that StackModelToValue and StackModelFromValue are inverses for an empty model.
func TestStackModelRoundTrip(t *testing.T) {
	original := provider.StackModel{}
	v, err := StackModelToValue(original)
	if err != nil {
		t.Fatalf("ToValue error: %v", err)
	}
	got, err := StackModelFromValue(v)
	if err != nil {
		t.Fatalf("FromValue error: %v", err)
	}
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("round-trip mismatch:\n got: %+v\nwant: %+v", got, original)
	}
}
// TestStackModelSpecType verifies that the generated StackModelSpecType function returns an object type.
func TestStackModelSpecType(t *testing.T) {
	typ := StackModelSpecType()
	if typ == nil {
		t.Fatal("Type() returned nil")
	}
}
// TestStackModelSpecRoundTrip verifies that StackModelSpecToValue and StackModelSpecFromValue are inverses for an empty model.
func TestStackModelSpecRoundTrip(t *testing.T) {
	original := provider.StackModelSpec{}
	v, err := StackModelSpecToValue(original)
	if err != nil {
		t.Fatalf("ToValue error: %v", err)
	}
	got, err := StackModelSpecFromValue(v)
	if err != nil {
		t.Fatalf("FromValue error: %v", err)
	}
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("round-trip mismatch:\n got: %+v\nwant: %+v", got, original)
	}
}
// TestStackModelStatusType verifies that the generated StackModelStatusType function returns an object type.
func TestStackModelStatusType(t *testing.T) {
	typ := StackModelStatusType()
	if typ == nil {
		t.Fatal("Type() returned nil")
	}
}
// TestStackModelStatusRoundTrip verifies that StackModelStatusToValue and StackModelStatusFromValue are inverses for an empty model.
func TestStackModelStatusRoundTrip(t *testing.T) {
	original := provider.StackModelStatus{}
	v, err := StackModelStatusToValue(original)
	if err != nil {
		t.Fatalf("ToValue error: %v", err)
	}
	got, err := StackModelStatusFromValue(v)
	if err != nil {
		t.Fatalf("FromValue error: %v", err)
	}
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("round-trip mismatch:\n got: %+v\nwant: %+v", got, original)
	}
}
// TestWorkspaceModelType verifies that the generated WorkspaceModelType function returns an object type.
func TestWorkspaceModelType(t *testing.T) {
	typ := WorkspaceModelType()
	if typ == nil {
		t.Fatal("Type() returned nil")
	}
}
// TestWorkspaceModelRoundTrip verifies that WorkspaceModelToValue and WorkspaceModelFromValue are inverses for an empty model.
func TestWorkspaceModelRoundTrip(t *testing.T) {
	original := provider.WorkspaceModel{}
	v, err := WorkspaceModelToValue(original)
	if err != nil {
		t.Fatalf("ToValue error: %v", err)
	}
	got, err := WorkspaceModelFromValue(v)
	if err != nil {
		t.Fatalf("FromValue error: %v", err)
	}
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("round-trip mismatch:\n got: %+v\nwant: %+v", got, original)
	}
}
// TestWorkspaceModelStatusType verifies that the generated WorkspaceModelStatusType function returns an object type.
func TestWorkspaceModelStatusType(t *testing.T) {
	typ := WorkspaceModelStatusType()
	if typ == nil {
		t.Fatal("Type() returned nil")
	}
}
// TestWorkspaceModelStatusRoundTrip verifies that WorkspaceModelStatusToValue and WorkspaceModelStatusFromValue are inverses for an empty model.
func TestWorkspaceModelStatusRoundTrip(t *testing.T) {
	original := provider.WorkspaceModelStatus{}
	v, err := WorkspaceModelStatusToValue(original)
	if err != nil {
		t.Fatalf("ToValue error: %v", err)
	}
	got, err := WorkspaceModelStatusFromValue(v)
	if err != nil {
		t.Fatalf("FromValue error: %v", err)
	}
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("round-trip mismatch:\n got: %+v\nwant: %+v", got, original)
	}
}
// TestCreatePullRequestModelType verifies that the generated CreatePullRequestModelType function returns an object type.
func TestCreatePullRequestModelType(t *testing.T) {
	typ := CreatePullRequestModelType()
	if typ == nil {
		t.Fatal("Type() returned nil")
	}
}
// TestCreatePullRequestModelRoundTrip verifies that CreatePullRequestModelToValue and CreatePullRequestModelFromValue are inverses for an empty model.
func TestCreatePullRequestModelRoundTrip(t *testing.T) {
	original := provider.CreatePullRequestModel{}
	v, err := CreatePullRequestModelToValue(original)
	if err != nil {
		t.Fatalf("ToValue error: %v", err)
	}
	got, err := CreatePullRequestModelFromValue(v)
	if err != nil {
		t.Fatalf("FromValue error: %v", err)
	}
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("round-trip mismatch:\n got: %+v\nwant: %+v", got, original)
	}
}
// TestUpdatePullRequestModelType verifies that the generated UpdatePullRequestModelType function returns an object type.
func TestUpdatePullRequestModelType(t *testing.T) {
	typ := UpdatePullRequestModelType()
	if typ == nil {
		t.Fatal("Type() returned nil")
	}
}
// TestUpdatePullRequestModelRoundTrip verifies that UpdatePullRequestModelToValue and UpdatePullRequestModelFromValue are inverses for an empty model.
func TestUpdatePullRequestModelRoundTrip(t *testing.T) {
	original := provider.UpdatePullRequestModel{}
	v, err := UpdatePullRequestModelToValue(original)
	if err != nil {
		t.Fatalf("ToValue error: %v", err)
	}
	got, err := UpdatePullRequestModelFromValue(v)
	if err != nil {
		t.Fatalf("FromValue error: %v", err)
	}
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("round-trip mismatch:\n got: %+v\nwant: %+v", got, original)
	}
}
// TestCreateTaskModelType verifies that the generated CreateTaskModelType function returns an object type.
func TestCreateTaskModelType(t *testing.T) {
	typ := CreateTaskModelType()
	if typ == nil {
		t.Fatal("Type() returned nil")
	}
}
// TestCreateTaskModelRoundTrip verifies that CreateTaskModelToValue and CreateTaskModelFromValue are inverses for an empty model.
func TestCreateTaskModelRoundTrip(t *testing.T) {
	original := provider.CreateTaskModel{}
	v, err := CreateTaskModelToValue(original)
	if err != nil {
		t.Fatalf("ToValue error: %v", err)
	}
	got, err := CreateTaskModelFromValue(v)
	if err != nil {
		t.Fatalf("FromValue error: %v", err)
	}
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("round-trip mismatch:\n got: %+v\nwant: %+v", got, original)
	}
}
// TestUpdateTaskModelType verifies that the generated UpdateTaskModelType function returns an object type.
func TestUpdateTaskModelType(t *testing.T) {
	typ := UpdateTaskModelType()
	if typ == nil {
		t.Fatal("Type() returned nil")
	}
}
// TestUpdateTaskModelRoundTrip verifies that UpdateTaskModelToValue and UpdateTaskModelFromValue are inverses for an empty model.
func TestUpdateTaskModelRoundTrip(t *testing.T) {
	original := provider.UpdateTaskModel{}
	v, err := UpdateTaskModelToValue(original)
	if err != nil {
		t.Fatalf("ToValue error: %v", err)
	}
	got, err := UpdateTaskModelFromValue(v)
	if err != nil {
		t.Fatalf("FromValue error: %v", err)
	}
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("round-trip mismatch:\n got: %+v\nwant: %+v", got, original)
	}
}
