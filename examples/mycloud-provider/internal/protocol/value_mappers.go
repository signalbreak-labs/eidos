package protocol

import (
	"fmt"
	"math"
	"math/big"
)
import (
	tftypes "github.com/hashicorp/terraform-plugin-go/tftypes"
	provider "github.com/mycloud/terraform-provider-mycloud/internal/provider"
)
// decodeString decodes a non-null string value into *out. Null values leave *out unchanged and return nil, preserving partial-state tolerance.
func decodeString(v tftypes.Value, out *string) error {
	if v.IsNull() {
		return nil
	}
	return v.As(out)
}
// decodeStringPtr decodes a string value, returning nil for null values without error so optional fields remain nil.
func decodeStringPtr(v tftypes.Value) (*string, error) {
	if v.IsNull() {
		return nil, nil
	}
	var s string
	if err := v.As(&s); err != nil {
		return nil, err
	}
	return &s, nil
}
// decodeInt64 decodes a non-null tftypes.Number into *out as int64. Null values leave *out at zero and return nil, preserving partial-state tolerance. NewFloat is allocated per call because big.Float is mutable and not safe for concurrent reuse.
func decodeInt64(v tftypes.Value, out *int64) error {
	if v.IsNull() {
		return nil
	}
	bf := big.NewFloat(0)
	if err := v.As(&bf); err != nil {
		return err
	}
	i, acc := bf.Int64()
	if acc != big.Exact {
		return fmt.Errorf("decode int64: number %s is not an exact integer (accuracy %v)", bf.String(), acc)
	}
	*out = i
	return nil
}
// decodeInt64Ptr decodes a number value, returning nil for null values without error so optional fields remain nil.
func decodeInt64Ptr(v tftypes.Value) (*int64, error) {
	if v.IsNull() {
		return nil, nil
	}
	bf := big.NewFloat(0)
	if err := v.As(&bf); err != nil {
		return nil, err
	}
	i, acc := bf.Int64()
	if acc != big.Exact {
		return nil, fmt.Errorf("decode int64: number %s is not an exact integer (accuracy %v)", bf.String(), acc)
	}
	return &i, nil
}
// decodeFloat64 decodes a non-null tftypes.Number into *out as float64. Null values leave *out at zero and return nil, preserving partial-state tolerance. NewFloat is allocated per call because big.Float is mutable and not safe for concurrent reuse.
func decodeFloat64(v tftypes.Value, out *float64) error {
	if v.IsNull() {
		return nil
	}
	bf := big.NewFloat(0)
	if err := v.As(&bf); err != nil {
		return err
	}
	f, _ := bf.Float64()
	if math.IsInf(f, 0) {
		return fmt.Errorf("decode float64: number %s is out of float64 range", bf.String())
	}
	*out = f
	return nil
}
// decodeFloat64Ptr decodes a number value, returning nil for null values without error so optional fields remain nil.
func decodeFloat64Ptr(v tftypes.Value) (*float64, error) {
	if v.IsNull() {
		return nil, nil
	}
	bf := big.NewFloat(0)
	if err := v.As(&bf); err != nil {
		return nil, err
	}
	f, _ := bf.Float64()
	if math.IsInf(f, 0) {
		return nil, fmt.Errorf("decode float64: number %s is out of float64 range", bf.String())
	}
	return &f, nil
}
// decodeBool decodes a non-null bool value into *out. Null values leave *out unchanged and return nil, preserving partial-state tolerance.
func decodeBool(v tftypes.Value, out *bool) error {
	if v.IsNull() {
		return nil
	}
	return v.As(out)
}
// decodeBoolPtr decodes a bool value, returning nil for null values without error so optional fields remain nil.
func decodeBoolPtr(v tftypes.Value) (*bool, error) {
	if v.IsNull() {
		return nil, nil
	}
	var b bool
	if err := v.As(&b); err != nil {
		return nil, err
	}
	return &b, nil
}
func ConfigModelType() tftypes.Type {
	return tftypes.Object{AttributeTypes: map[string]tftypes.Type{"api_version": tftypes.String, "data": tftypes.Map{ElementType: tftypes.String}, "id": tftypes.String, "kind": tftypes.String, "name": tftypes.String, "workspace": tftypes.String}}
}
func ConfigModelFromValue(v tftypes.Value) (provider.ConfigModel, error) {
	var m provider.ConfigModel
	if v.IsNull() {
		return m, nil
	}
	if v.IsKnown() != true {
		return m, fmt.Errorf("cannot decode unknown %s value", "ConfigModel")
	}
	var vals map[string]tftypes.Value
	if err := v.As(&vals); err != nil {
		return m, err
	}
	if val, ok := vals["api_version"]; ok {
		v, err := decodeStringPtr(val)
		if err != nil {
			return m, err
		}
		m.ApiVersion = v
	}
	if val, ok := vals["data"]; ok && !val.IsNull() {
		var elems map[string]tftypes.Value
		if err := val.As(&elems); err != nil {
			return m, err
		}
		m.Data = make(map[string]string, len(elems))
		for k, ev := range elems {
			var tmp string
			if err := decodeString(ev, &tmp); err != nil {
				return m, err
			}
			m.Data[k] = tmp
		}
	}
	if val, ok := vals["id"]; ok {
		v, err := decodeStringPtr(val)
		if err != nil {
			return m, err
		}
		m.Id = v
	}
	if val, ok := vals["kind"]; ok {
		v, err := decodeStringPtr(val)
		if err != nil {
			return m, err
		}
		m.Kind = v
	}
	if val, ok := vals["name"]; ok {
		if err := decodeString(val, &m.Name); err != nil {
			return m, err
		}
	} else {
		return m, fmt.Errorf("missing required attribute %q", "name")
	}
	if val, ok := vals["workspace"]; ok {
		if err := decodeString(val, &m.Workspace); err != nil {
			return m, err
		}
	} else {
		return m, fmt.Errorf("missing required attribute %q", "workspace")
	}
	return m, nil
}
func ConfigModelToValue(m provider.ConfigModel) (tftypes.Value, error) {
	vals := map[string]tftypes.Value{}
	if m.ApiVersion != nil {
		vals["api_version"] = tftypes.NewValue(tftypes.String, *m.ApiVersion)
	} else {
		vals["api_version"] = tftypes.NewValue(tftypes.String, nil)
	}
	if m.Data != nil {
		elems := make(map[string]tftypes.Value, len(m.Data))
		for k, v := range m.Data {
			elems[k] = tftypes.NewValue(tftypes.String, v)
		}
		vals["data"] = tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, elems)
	} else {
		vals["data"] = tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, nil)
	}
	if m.Id != nil {
		vals["id"] = tftypes.NewValue(tftypes.String, *m.Id)
	} else {
		vals["id"] = tftypes.NewValue(tftypes.String, nil)
	}
	if m.Kind != nil {
		vals["kind"] = tftypes.NewValue(tftypes.String, *m.Kind)
	} else {
		vals["kind"] = tftypes.NewValue(tftypes.String, nil)
	}
	vals["name"] = tftypes.NewValue(tftypes.String, m.Name)
	vals["workspace"] = tftypes.NewValue(tftypes.String, m.Workspace)
	return tftypes.NewValue(ConfigModelType(), vals), nil
}
func InstanceModelSpecContainersElemType() tftypes.Type {
	return tftypes.Object{AttributeTypes: map[string]tftypes.Type{"image": tftypes.String, "image_pull_policy": tftypes.String, "name": tftypes.String}}
}
func InstanceModelSpecContainersElemFromValue(v tftypes.Value) (provider.InstanceModelSpecContainersElem, error) {
	var m provider.InstanceModelSpecContainersElem
	if v.IsNull() {
		return m, nil
	}
	if v.IsKnown() != true {
		return m, fmt.Errorf("cannot decode unknown %s value", "InstanceModelSpecContainersElem")
	}
	var vals map[string]tftypes.Value
	if err := v.As(&vals); err != nil {
		return m, err
	}
	if val, ok := vals["image"]; ok {
		v, err := decodeStringPtr(val)
		if err != nil {
			return m, err
		}
		m.Image = v
	}
	if val, ok := vals["image_pull_policy"]; ok {
		v, err := decodeStringPtr(val)
		if err != nil {
			return m, err
		}
		m.ImagePullPolicy = v
	}
	if val, ok := vals["name"]; ok {
		v, err := decodeStringPtr(val)
		if err != nil {
			return m, err
		}
		m.Name = v
	}
	return m, nil
}
func InstanceModelSpecContainersElemToValue(m provider.InstanceModelSpecContainersElem) (tftypes.Value, error) {
	vals := map[string]tftypes.Value{}
	if m.Image != nil {
		vals["image"] = tftypes.NewValue(tftypes.String, *m.Image)
	} else {
		vals["image"] = tftypes.NewValue(tftypes.String, nil)
	}
	if m.ImagePullPolicy != nil {
		vals["image_pull_policy"] = tftypes.NewValue(tftypes.String, *m.ImagePullPolicy)
	} else {
		vals["image_pull_policy"] = tftypes.NewValue(tftypes.String, nil)
	}
	if m.Name != nil {
		vals["name"] = tftypes.NewValue(tftypes.String, *m.Name)
	} else {
		vals["name"] = tftypes.NewValue(tftypes.String, nil)
	}
	return tftypes.NewValue(InstanceModelSpecContainersElemType(), vals), nil
}
func InstanceModelSpecType() tftypes.Type {
	return tftypes.Object{AttributeTypes: map[string]tftypes.Type{"containers": tftypes.List{ElementType: InstanceModelSpecContainersElemType()}}}
}
func InstanceModelSpecFromValue(v tftypes.Value) (provider.InstanceModelSpec, error) {
	var m provider.InstanceModelSpec
	if v.IsNull() {
		return m, nil
	}
	if v.IsKnown() != true {
		return m, fmt.Errorf("cannot decode unknown %s value", "InstanceModelSpec")
	}
	var vals map[string]tftypes.Value
	if err := v.As(&vals); err != nil {
		return m, err
	}
	if val, ok := vals["containers"]; ok && !val.IsNull() {
		var elems []tftypes.Value
		if err := val.As(&elems); err != nil {
			return m, err
		}
		m.Containers = make([]provider.InstanceModelSpecContainersElem, len(elems))
		for i, ev := range elems {
			nested, err := InstanceModelSpecContainersElemFromValue(ev)
			if err != nil {
				return m, err
			}
			m.Containers[i] = nested
		}
	}
	return m, nil
}
func InstanceModelSpecToValue(m provider.InstanceModelSpec) (tftypes.Value, error) {
	vals := map[string]tftypes.Value{}
	if m.Containers != nil {
		elems := make([]tftypes.Value, len(m.Containers))
		for i, v := range m.Containers {
			ev, err := InstanceModelSpecContainersElemToValue(v)
			if err != nil {
				return tftypes.Value{}, err
			}
			elems[i] = ev
		}
		vals["containers"] = tftypes.NewValue(tftypes.List{ElementType: InstanceModelSpecContainersElemType()}, elems)
	} else {
		vals["containers"] = tftypes.NewValue(tftypes.List{ElementType: InstanceModelSpecContainersElemType()}, nil)
	}
	return tftypes.NewValue(InstanceModelSpecType(), vals), nil
}
func InstanceModelStatusType() tftypes.Type {
	return tftypes.Object{AttributeTypes: map[string]tftypes.Type{"phase": tftypes.String}}
}
func InstanceModelStatusFromValue(v tftypes.Value) (provider.InstanceModelStatus, error) {
	var m provider.InstanceModelStatus
	if v.IsNull() {
		return m, nil
	}
	if v.IsKnown() != true {
		return m, fmt.Errorf("cannot decode unknown %s value", "InstanceModelStatus")
	}
	var vals map[string]tftypes.Value
	if err := v.As(&vals); err != nil {
		return m, err
	}
	if val, ok := vals["phase"]; ok {
		v, err := decodeStringPtr(val)
		if err != nil {
			return m, err
		}
		m.Phase = v
	}
	return m, nil
}
func InstanceModelStatusToValue(m provider.InstanceModelStatus) (tftypes.Value, error) {
	vals := map[string]tftypes.Value{}
	if m.Phase != nil {
		vals["phase"] = tftypes.NewValue(tftypes.String, *m.Phase)
	} else {
		vals["phase"] = tftypes.NewValue(tftypes.String, nil)
	}
	return tftypes.NewValue(InstanceModelStatusType(), vals), nil
}
func InstanceModelType() tftypes.Type {
	return tftypes.Object{AttributeTypes: map[string]tftypes.Type{"api_version": tftypes.String, "id": tftypes.String, "kind": tftypes.String, "labels": tftypes.Map{ElementType: tftypes.String}, "name": tftypes.String, "spec": InstanceModelSpecType(), "status": InstanceModelStatusType(), "workspace": tftypes.String}}
}
func InstanceModelFromValue(v tftypes.Value) (provider.InstanceModel, error) {
	var m provider.InstanceModel
	if v.IsNull() {
		return m, nil
	}
	if v.IsKnown() != true {
		return m, fmt.Errorf("cannot decode unknown %s value", "InstanceModel")
	}
	var vals map[string]tftypes.Value
	if err := v.As(&vals); err != nil {
		return m, err
	}
	if val, ok := vals["api_version"]; ok {
		v, err := decodeStringPtr(val)
		if err != nil {
			return m, err
		}
		m.ApiVersion = v
	}
	if val, ok := vals["id"]; ok {
		v, err := decodeStringPtr(val)
		if err != nil {
			return m, err
		}
		m.Id = v
	}
	if val, ok := vals["kind"]; ok {
		v, err := decodeStringPtr(val)
		if err != nil {
			return m, err
		}
		m.Kind = v
	}
	if val, ok := vals["labels"]; ok && !val.IsNull() {
		var elems map[string]tftypes.Value
		if err := val.As(&elems); err != nil {
			return m, err
		}
		m.Labels = make(map[string]string, len(elems))
		for k, ev := range elems {
			var tmp string
			if err := decodeString(ev, &tmp); err != nil {
				return m, err
			}
			m.Labels[k] = tmp
		}
	}
	if val, ok := vals["name"]; ok {
		if err := decodeString(val, &m.Name); err != nil {
			return m, err
		}
	} else {
		return m, fmt.Errorf("missing required attribute %q", "name")
	}
	if val, ok := vals["spec"]; ok && !val.IsNull() {
		nested, err := InstanceModelSpecFromValue(val)
		if err != nil {
			return m, err
		}
		m.Spec = &nested
	}
	if val, ok := vals["status"]; ok && !val.IsNull() {
		nested, err := InstanceModelStatusFromValue(val)
		if err != nil {
			return m, err
		}
		m.Status = &nested
	}
	if val, ok := vals["workspace"]; ok {
		if err := decodeString(val, &m.Workspace); err != nil {
			return m, err
		}
	} else {
		return m, fmt.Errorf("missing required attribute %q", "workspace")
	}
	return m, nil
}
func InstanceModelToValue(m provider.InstanceModel) (tftypes.Value, error) {
	vals := map[string]tftypes.Value{}
	if m.ApiVersion != nil {
		vals["api_version"] = tftypes.NewValue(tftypes.String, *m.ApiVersion)
	} else {
		vals["api_version"] = tftypes.NewValue(tftypes.String, nil)
	}
	if m.Id != nil {
		vals["id"] = tftypes.NewValue(tftypes.String, *m.Id)
	} else {
		vals["id"] = tftypes.NewValue(tftypes.String, nil)
	}
	if m.Kind != nil {
		vals["kind"] = tftypes.NewValue(tftypes.String, *m.Kind)
	} else {
		vals["kind"] = tftypes.NewValue(tftypes.String, nil)
	}
	if m.Labels != nil {
		elems := make(map[string]tftypes.Value, len(m.Labels))
		for k, v := range m.Labels {
			elems[k] = tftypes.NewValue(tftypes.String, v)
		}
		vals["labels"] = tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, elems)
	} else {
		vals["labels"] = tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, nil)
	}
	vals["name"] = tftypes.NewValue(tftypes.String, m.Name)
	if m.Spec != nil {
		nested, err := InstanceModelSpecToValue(*m.Spec)
		if err != nil {
			return tftypes.Value{}, err
		}
		vals["spec"] = nested
	} else {
		vals["spec"] = tftypes.NewValue(InstanceModelSpecType(), nil)
	}
	if m.Status != nil {
		nested, err := InstanceModelStatusToValue(*m.Status)
		if err != nil {
			return tftypes.Value{}, err
		}
		vals["status"] = nested
	} else {
		vals["status"] = tftypes.NewValue(InstanceModelStatusType(), nil)
	}
	vals["workspace"] = tftypes.NewValue(tftypes.String, m.Workspace)
	return tftypes.NewValue(InstanceModelType(), vals), nil
}
func NetworkModelSpecPortsElemType() tftypes.Type {
	return tftypes.Object{AttributeTypes: map[string]tftypes.Type{"name": tftypes.String, "port": tftypes.Number, "protocol": tftypes.String}}
}
func NetworkModelSpecPortsElemFromValue(v tftypes.Value) (provider.NetworkModelSpecPortsElem, error) {
	var m provider.NetworkModelSpecPortsElem
	if v.IsNull() {
		return m, nil
	}
	if v.IsKnown() != true {
		return m, fmt.Errorf("cannot decode unknown %s value", "NetworkModelSpecPortsElem")
	}
	var vals map[string]tftypes.Value
	if err := v.As(&vals); err != nil {
		return m, err
	}
	if val, ok := vals["name"]; ok {
		v, err := decodeStringPtr(val)
		if err != nil {
			return m, err
		}
		m.Name = v
	}
	if val, ok := vals["port"]; ok {
		v, err := decodeInt64Ptr(val)
		if err != nil {
			return m, err
		}
		m.Port = v
	}
	if val, ok := vals["protocol"]; ok {
		v, err := decodeStringPtr(val)
		if err != nil {
			return m, err
		}
		m.Protocol = v
	}
	return m, nil
}
func NetworkModelSpecPortsElemToValue(m provider.NetworkModelSpecPortsElem) (tftypes.Value, error) {
	vals := map[string]tftypes.Value{}
	if m.Name != nil {
		vals["name"] = tftypes.NewValue(tftypes.String, *m.Name)
	} else {
		vals["name"] = tftypes.NewValue(tftypes.String, nil)
	}
	if m.Port != nil {
		vals["port"] = tftypes.NewValue(tftypes.Number, *m.Port)
	} else {
		vals["port"] = tftypes.NewValue(tftypes.Number, nil)
	}
	if m.Protocol != nil {
		vals["protocol"] = tftypes.NewValue(tftypes.String, *m.Protocol)
	} else {
		vals["protocol"] = tftypes.NewValue(tftypes.String, nil)
	}
	return tftypes.NewValue(NetworkModelSpecPortsElemType(), vals), nil
}
func NetworkModelSpecType() tftypes.Type {
	return tftypes.Object{AttributeTypes: map[string]tftypes.Type{"ip_address": tftypes.String, "ports": tftypes.List{ElementType: NetworkModelSpecPortsElemType()}, "selector": tftypes.Map{ElementType: tftypes.String}}}
}
func NetworkModelSpecFromValue(v tftypes.Value) (provider.NetworkModelSpec, error) {
	var m provider.NetworkModelSpec
	if v.IsNull() {
		return m, nil
	}
	if v.IsKnown() != true {
		return m, fmt.Errorf("cannot decode unknown %s value", "NetworkModelSpec")
	}
	var vals map[string]tftypes.Value
	if err := v.As(&vals); err != nil {
		return m, err
	}
	if val, ok := vals["ip_address"]; ok {
		v, err := decodeStringPtr(val)
		if err != nil {
			return m, err
		}
		m.IpAddress = v
	}
	if val, ok := vals["ports"]; ok && !val.IsNull() {
		var elems []tftypes.Value
		if err := val.As(&elems); err != nil {
			return m, err
		}
		m.Ports = make([]provider.NetworkModelSpecPortsElem, len(elems))
		for i, ev := range elems {
			nested, err := NetworkModelSpecPortsElemFromValue(ev)
			if err != nil {
				return m, err
			}
			m.Ports[i] = nested
		}
	}
	if val, ok := vals["selector"]; ok && !val.IsNull() {
		var elems map[string]tftypes.Value
		if err := val.As(&elems); err != nil {
			return m, err
		}
		m.Selector = make(map[string]string, len(elems))
		for k, ev := range elems {
			var tmp string
			if err := decodeString(ev, &tmp); err != nil {
				return m, err
			}
			m.Selector[k] = tmp
		}
	}
	return m, nil
}
func NetworkModelSpecToValue(m provider.NetworkModelSpec) (tftypes.Value, error) {
	vals := map[string]tftypes.Value{}
	if m.IpAddress != nil {
		vals["ip_address"] = tftypes.NewValue(tftypes.String, *m.IpAddress)
	} else {
		vals["ip_address"] = tftypes.NewValue(tftypes.String, nil)
	}
	if m.Ports != nil {
		elems := make([]tftypes.Value, len(m.Ports))
		for i, v := range m.Ports {
			ev, err := NetworkModelSpecPortsElemToValue(v)
			if err != nil {
				return tftypes.Value{}, err
			}
			elems[i] = ev
		}
		vals["ports"] = tftypes.NewValue(tftypes.List{ElementType: NetworkModelSpecPortsElemType()}, elems)
	} else {
		vals["ports"] = tftypes.NewValue(tftypes.List{ElementType: NetworkModelSpecPortsElemType()}, nil)
	}
	if m.Selector != nil {
		elems := make(map[string]tftypes.Value, len(m.Selector))
		for k, v := range m.Selector {
			elems[k] = tftypes.NewValue(tftypes.String, v)
		}
		vals["selector"] = tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, elems)
	} else {
		vals["selector"] = tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, nil)
	}
	return tftypes.NewValue(NetworkModelSpecType(), vals), nil
}
func NetworkModelStatusType() tftypes.Type {
	return tftypes.Object{AttributeTypes: map[string]tftypes.Type{"load_balancer": tftypes.DynamicPseudoType}}
}
func NetworkModelStatusFromValue(v tftypes.Value) (provider.NetworkModelStatus, error) {
	var m provider.NetworkModelStatus
	if v.IsNull() {
		return m, nil
	}
	if v.IsKnown() != true {
		return m, fmt.Errorf("cannot decode unknown %s value", "NetworkModelStatus")
	}
	var vals map[string]tftypes.Value
	if err := v.As(&vals); err != nil {
		return m, err
	}
	if val, ok := vals["load_balancer"]; ok {
		m.LoadBalancer = val
	}
	return m, nil
}
func NetworkModelStatusToValue(m provider.NetworkModelStatus) (tftypes.Value, error) {
	vals := map[string]tftypes.Value{}
	if m.LoadBalancer.IsNull() {
		vals["load_balancer"] = tftypes.NewValue(tftypes.DynamicPseudoType, nil)
	} else {
		vals["load_balancer"] = m.LoadBalancer
	}
	return tftypes.NewValue(NetworkModelStatusType(), vals), nil
}
func NetworkModelType() tftypes.Type {
	return tftypes.Object{AttributeTypes: map[string]tftypes.Type{"api_version": tftypes.String, "id": tftypes.String, "kind": tftypes.String, "name": tftypes.String, "spec": NetworkModelSpecType(), "status": NetworkModelStatusType(), "workspace": tftypes.String}}
}
func NetworkModelFromValue(v tftypes.Value) (provider.NetworkModel, error) {
	var m provider.NetworkModel
	if v.IsNull() {
		return m, nil
	}
	if v.IsKnown() != true {
		return m, fmt.Errorf("cannot decode unknown %s value", "NetworkModel")
	}
	var vals map[string]tftypes.Value
	if err := v.As(&vals); err != nil {
		return m, err
	}
	if val, ok := vals["api_version"]; ok {
		v, err := decodeStringPtr(val)
		if err != nil {
			return m, err
		}
		m.ApiVersion = v
	}
	if val, ok := vals["id"]; ok {
		v, err := decodeStringPtr(val)
		if err != nil {
			return m, err
		}
		m.Id = v
	}
	if val, ok := vals["kind"]; ok {
		v, err := decodeStringPtr(val)
		if err != nil {
			return m, err
		}
		m.Kind = v
	}
	if val, ok := vals["name"]; ok {
		if err := decodeString(val, &m.Name); err != nil {
			return m, err
		}
	} else {
		return m, fmt.Errorf("missing required attribute %q", "name")
	}
	if val, ok := vals["spec"]; ok && !val.IsNull() {
		nested, err := NetworkModelSpecFromValue(val)
		if err != nil {
			return m, err
		}
		m.Spec = &nested
	}
	if val, ok := vals["status"]; ok && !val.IsNull() {
		nested, err := NetworkModelStatusFromValue(val)
		if err != nil {
			return m, err
		}
		m.Status = &nested
	}
	if val, ok := vals["workspace"]; ok {
		if err := decodeString(val, &m.Workspace); err != nil {
			return m, err
		}
	} else {
		return m, fmt.Errorf("missing required attribute %q", "workspace")
	}
	return m, nil
}
func NetworkModelToValue(m provider.NetworkModel) (tftypes.Value, error) {
	vals := map[string]tftypes.Value{}
	if m.ApiVersion != nil {
		vals["api_version"] = tftypes.NewValue(tftypes.String, *m.ApiVersion)
	} else {
		vals["api_version"] = tftypes.NewValue(tftypes.String, nil)
	}
	if m.Id != nil {
		vals["id"] = tftypes.NewValue(tftypes.String, *m.Id)
	} else {
		vals["id"] = tftypes.NewValue(tftypes.String, nil)
	}
	if m.Kind != nil {
		vals["kind"] = tftypes.NewValue(tftypes.String, *m.Kind)
	} else {
		vals["kind"] = tftypes.NewValue(tftypes.String, nil)
	}
	vals["name"] = tftypes.NewValue(tftypes.String, m.Name)
	if m.Spec != nil {
		nested, err := NetworkModelSpecToValue(*m.Spec)
		if err != nil {
			return tftypes.Value{}, err
		}
		vals["spec"] = nested
	} else {
		vals["spec"] = tftypes.NewValue(NetworkModelSpecType(), nil)
	}
	if m.Status != nil {
		nested, err := NetworkModelStatusToValue(*m.Status)
		if err != nil {
			return tftypes.Value{}, err
		}
		vals["status"] = nested
	} else {
		vals["status"] = tftypes.NewValue(NetworkModelStatusType(), nil)
	}
	vals["workspace"] = tftypes.NewValue(tftypes.String, m.Workspace)
	return tftypes.NewValue(NetworkModelType(), vals), nil
}
func ProjectModelType() tftypes.Type {
	return tftypes.Object{AttributeTypes: map[string]tftypes.Type{"default_branch": tftypes.String, "description": tftypes.String, "full_name": tftypes.String, "html_url": tftypes.String, "id": tftypes.Number, "name": tftypes.String, "organization": tftypes.String, "private": tftypes.Bool, "project": tftypes.String}}
}
func ProjectModelFromValue(v tftypes.Value) (provider.ProjectModel, error) {
	var m provider.ProjectModel
	if v.IsNull() {
		return m, nil
	}
	if v.IsKnown() != true {
		return m, fmt.Errorf("cannot decode unknown %s value", "ProjectModel")
	}
	var vals map[string]tftypes.Value
	if err := v.As(&vals); err != nil {
		return m, err
	}
	if val, ok := vals["default_branch"]; ok {
		v, err := decodeStringPtr(val)
		if err != nil {
			return m, err
		}
		m.DefaultBranch = v
	}
	if val, ok := vals["description"]; ok {
		v, err := decodeStringPtr(val)
		if err != nil {
			return m, err
		}
		m.Description = v
	}
	if val, ok := vals["full_name"]; ok {
		v, err := decodeStringPtr(val)
		if err != nil {
			return m, err
		}
		m.FullName = v
	}
	if val, ok := vals["html_url"]; ok {
		v, err := decodeStringPtr(val)
		if err != nil {
			return m, err
		}
		m.HtmlUrl = v
	}
	if val, ok := vals["id"]; ok {
		v, err := decodeInt64Ptr(val)
		if err != nil {
			return m, err
		}
		m.Id = v
	}
	if val, ok := vals["name"]; ok {
		v, err := decodeStringPtr(val)
		if err != nil {
			return m, err
		}
		m.Name = v
	}
	if val, ok := vals["organization"]; ok {
		v, err := decodeStringPtr(val)
		if err != nil {
			return m, err
		}
		m.Organization = v
	}
	if val, ok := vals["private"]; ok {
		v, err := decodeBoolPtr(val)
		if err != nil {
			return m, err
		}
		m.Private = v
	}
	if val, ok := vals["project"]; ok {
		v, err := decodeStringPtr(val)
		if err != nil {
			return m, err
		}
		m.Project = v
	}
	return m, nil
}
func ProjectModelToValue(m provider.ProjectModel) (tftypes.Value, error) {
	vals := map[string]tftypes.Value{}
	if m.DefaultBranch != nil {
		vals["default_branch"] = tftypes.NewValue(tftypes.String, *m.DefaultBranch)
	} else {
		vals["default_branch"] = tftypes.NewValue(tftypes.String, nil)
	}
	if m.Description != nil {
		vals["description"] = tftypes.NewValue(tftypes.String, *m.Description)
	} else {
		vals["description"] = tftypes.NewValue(tftypes.String, nil)
	}
	if m.FullName != nil {
		vals["full_name"] = tftypes.NewValue(tftypes.String, *m.FullName)
	} else {
		vals["full_name"] = tftypes.NewValue(tftypes.String, nil)
	}
	if m.HtmlUrl != nil {
		vals["html_url"] = tftypes.NewValue(tftypes.String, *m.HtmlUrl)
	} else {
		vals["html_url"] = tftypes.NewValue(tftypes.String, nil)
	}
	if m.Id != nil {
		vals["id"] = tftypes.NewValue(tftypes.Number, *m.Id)
	} else {
		vals["id"] = tftypes.NewValue(tftypes.Number, nil)
	}
	if m.Name != nil {
		vals["name"] = tftypes.NewValue(tftypes.String, *m.Name)
	} else {
		vals["name"] = tftypes.NewValue(tftypes.String, nil)
	}
	if m.Organization != nil {
		vals["organization"] = tftypes.NewValue(tftypes.String, *m.Organization)
	} else {
		vals["organization"] = tftypes.NewValue(tftypes.String, nil)
	}
	if m.Private != nil {
		vals["private"] = tftypes.NewValue(tftypes.Bool, *m.Private)
	} else {
		vals["private"] = tftypes.NewValue(tftypes.Bool, nil)
	}
	if m.Project != nil {
		vals["project"] = tftypes.NewValue(tftypes.String, *m.Project)
	} else {
		vals["project"] = tftypes.NewValue(tftypes.String, nil)
	}
	return tftypes.NewValue(ProjectModelType(), vals), nil
}
func SecretModelType() tftypes.Type {
	return tftypes.Object{AttributeTypes: map[string]tftypes.Type{"api_version": tftypes.String, "data": tftypes.Map{ElementType: tftypes.String}, "id": tftypes.String, "kind": tftypes.String, "name": tftypes.String, "type": tftypes.String, "workspace": tftypes.String}}
}
func SecretModelFromValue(v tftypes.Value) (provider.SecretModel, error) {
	var m provider.SecretModel
	if v.IsNull() {
		return m, nil
	}
	if v.IsKnown() != true {
		return m, fmt.Errorf("cannot decode unknown %s value", "SecretModel")
	}
	var vals map[string]tftypes.Value
	if err := v.As(&vals); err != nil {
		return m, err
	}
	if val, ok := vals["api_version"]; ok {
		v, err := decodeStringPtr(val)
		if err != nil {
			return m, err
		}
		m.ApiVersion = v
	}
	if val, ok := vals["data"]; ok && !val.IsNull() {
		var elems map[string]tftypes.Value
		if err := val.As(&elems); err != nil {
			return m, err
		}
		m.Data = make(map[string]string, len(elems))
		for k, ev := range elems {
			var tmp string
			if err := decodeString(ev, &tmp); err != nil {
				return m, err
			}
			m.Data[k] = tmp
		}
	}
	if val, ok := vals["id"]; ok {
		v, err := decodeStringPtr(val)
		if err != nil {
			return m, err
		}
		m.Id = v
	}
	if val, ok := vals["kind"]; ok {
		v, err := decodeStringPtr(val)
		if err != nil {
			return m, err
		}
		m.Kind = v
	}
	if val, ok := vals["name"]; ok {
		if err := decodeString(val, &m.Name); err != nil {
			return m, err
		}
	} else {
		return m, fmt.Errorf("missing required attribute %q", "name")
	}
	if val, ok := vals["type"]; ok {
		v, err := decodeStringPtr(val)
		if err != nil {
			return m, err
		}
		m.Type = v
	}
	if val, ok := vals["workspace"]; ok {
		if err := decodeString(val, &m.Workspace); err != nil {
			return m, err
		}
	} else {
		return m, fmt.Errorf("missing required attribute %q", "workspace")
	}
	return m, nil
}
func SecretModelToValue(m provider.SecretModel) (tftypes.Value, error) {
	vals := map[string]tftypes.Value{}
	if m.ApiVersion != nil {
		vals["api_version"] = tftypes.NewValue(tftypes.String, *m.ApiVersion)
	} else {
		vals["api_version"] = tftypes.NewValue(tftypes.String, nil)
	}
	if m.Data != nil {
		elems := make(map[string]tftypes.Value, len(m.Data))
		for k, v := range m.Data {
			elems[k] = tftypes.NewValue(tftypes.String, v)
		}
		vals["data"] = tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, elems)
	} else {
		vals["data"] = tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, nil)
	}
	if m.Id != nil {
		vals["id"] = tftypes.NewValue(tftypes.String, *m.Id)
	} else {
		vals["id"] = tftypes.NewValue(tftypes.String, nil)
	}
	if m.Kind != nil {
		vals["kind"] = tftypes.NewValue(tftypes.String, *m.Kind)
	} else {
		vals["kind"] = tftypes.NewValue(tftypes.String, nil)
	}
	vals["name"] = tftypes.NewValue(tftypes.String, m.Name)
	if m.Type != nil {
		vals["type"] = tftypes.NewValue(tftypes.String, *m.Type)
	} else {
		vals["type"] = tftypes.NewValue(tftypes.String, nil)
	}
	vals["workspace"] = tftypes.NewValue(tftypes.String, m.Workspace)
	return tftypes.NewValue(SecretModelType(), vals), nil
}
func StackModelSpecType() tftypes.Type {
	return tftypes.Object{AttributeTypes: map[string]tftypes.Type{"replicas": tftypes.Number, "selector": tftypes.Map{ElementType: tftypes.String}}}
}
func StackModelSpecFromValue(v tftypes.Value) (provider.StackModelSpec, error) {
	var m provider.StackModelSpec
	if v.IsNull() {
		return m, nil
	}
	if v.IsKnown() != true {
		return m, fmt.Errorf("cannot decode unknown %s value", "StackModelSpec")
	}
	var vals map[string]tftypes.Value
	if err := v.As(&vals); err != nil {
		return m, err
	}
	if val, ok := vals["replicas"]; ok {
		v, err := decodeInt64Ptr(val)
		if err != nil {
			return m, err
		}
		m.Replicas = v
	}
	if val, ok := vals["selector"]; ok && !val.IsNull() {
		var elems map[string]tftypes.Value
		if err := val.As(&elems); err != nil {
			return m, err
		}
		m.Selector = make(map[string]string, len(elems))
		for k, ev := range elems {
			var tmp string
			if err := decodeString(ev, &tmp); err != nil {
				return m, err
			}
			m.Selector[k] = tmp
		}
	}
	return m, nil
}
func StackModelSpecToValue(m provider.StackModelSpec) (tftypes.Value, error) {
	vals := map[string]tftypes.Value{}
	if m.Replicas != nil {
		vals["replicas"] = tftypes.NewValue(tftypes.Number, *m.Replicas)
	} else {
		vals["replicas"] = tftypes.NewValue(tftypes.Number, nil)
	}
	if m.Selector != nil {
		elems := make(map[string]tftypes.Value, len(m.Selector))
		for k, v := range m.Selector {
			elems[k] = tftypes.NewValue(tftypes.String, v)
		}
		vals["selector"] = tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, elems)
	} else {
		vals["selector"] = tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, nil)
	}
	return tftypes.NewValue(StackModelSpecType(), vals), nil
}
func StackModelStatusType() tftypes.Type {
	return tftypes.Object{AttributeTypes: map[string]tftypes.Type{"ready_replicas": tftypes.Number}}
}
func StackModelStatusFromValue(v tftypes.Value) (provider.StackModelStatus, error) {
	var m provider.StackModelStatus
	if v.IsNull() {
		return m, nil
	}
	if v.IsKnown() != true {
		return m, fmt.Errorf("cannot decode unknown %s value", "StackModelStatus")
	}
	var vals map[string]tftypes.Value
	if err := v.As(&vals); err != nil {
		return m, err
	}
	if val, ok := vals["ready_replicas"]; ok {
		v, err := decodeInt64Ptr(val)
		if err != nil {
			return m, err
		}
		m.ReadyReplicas = v
	}
	return m, nil
}
func StackModelStatusToValue(m provider.StackModelStatus) (tftypes.Value, error) {
	vals := map[string]tftypes.Value{}
	if m.ReadyReplicas != nil {
		vals["ready_replicas"] = tftypes.NewValue(tftypes.Number, *m.ReadyReplicas)
	} else {
		vals["ready_replicas"] = tftypes.NewValue(tftypes.Number, nil)
	}
	return tftypes.NewValue(StackModelStatusType(), vals), nil
}
func StackModelType() tftypes.Type {
	return tftypes.Object{AttributeTypes: map[string]tftypes.Type{"api_version": tftypes.String, "id": tftypes.String, "kind": tftypes.String, "name": tftypes.String, "spec": StackModelSpecType(), "status": StackModelStatusType(), "workspace": tftypes.String}}
}
func StackModelFromValue(v tftypes.Value) (provider.StackModel, error) {
	var m provider.StackModel
	if v.IsNull() {
		return m, nil
	}
	if v.IsKnown() != true {
		return m, fmt.Errorf("cannot decode unknown %s value", "StackModel")
	}
	var vals map[string]tftypes.Value
	if err := v.As(&vals); err != nil {
		return m, err
	}
	if val, ok := vals["api_version"]; ok {
		v, err := decodeStringPtr(val)
		if err != nil {
			return m, err
		}
		m.ApiVersion = v
	}
	if val, ok := vals["id"]; ok {
		v, err := decodeStringPtr(val)
		if err != nil {
			return m, err
		}
		m.Id = v
	}
	if val, ok := vals["kind"]; ok {
		v, err := decodeStringPtr(val)
		if err != nil {
			return m, err
		}
		m.Kind = v
	}
	if val, ok := vals["name"]; ok {
		if err := decodeString(val, &m.Name); err != nil {
			return m, err
		}
	} else {
		return m, fmt.Errorf("missing required attribute %q", "name")
	}
	if val, ok := vals["spec"]; ok && !val.IsNull() {
		nested, err := StackModelSpecFromValue(val)
		if err != nil {
			return m, err
		}
		m.Spec = &nested
	}
	if val, ok := vals["status"]; ok && !val.IsNull() {
		nested, err := StackModelStatusFromValue(val)
		if err != nil {
			return m, err
		}
		m.Status = &nested
	}
	if val, ok := vals["workspace"]; ok {
		if err := decodeString(val, &m.Workspace); err != nil {
			return m, err
		}
	} else {
		return m, fmt.Errorf("missing required attribute %q", "workspace")
	}
	return m, nil
}
func StackModelToValue(m provider.StackModel) (tftypes.Value, error) {
	vals := map[string]tftypes.Value{}
	if m.ApiVersion != nil {
		vals["api_version"] = tftypes.NewValue(tftypes.String, *m.ApiVersion)
	} else {
		vals["api_version"] = tftypes.NewValue(tftypes.String, nil)
	}
	if m.Id != nil {
		vals["id"] = tftypes.NewValue(tftypes.String, *m.Id)
	} else {
		vals["id"] = tftypes.NewValue(tftypes.String, nil)
	}
	if m.Kind != nil {
		vals["kind"] = tftypes.NewValue(tftypes.String, *m.Kind)
	} else {
		vals["kind"] = tftypes.NewValue(tftypes.String, nil)
	}
	vals["name"] = tftypes.NewValue(tftypes.String, m.Name)
	if m.Spec != nil {
		nested, err := StackModelSpecToValue(*m.Spec)
		if err != nil {
			return tftypes.Value{}, err
		}
		vals["spec"] = nested
	} else {
		vals["spec"] = tftypes.NewValue(StackModelSpecType(), nil)
	}
	if m.Status != nil {
		nested, err := StackModelStatusToValue(*m.Status)
		if err != nil {
			return tftypes.Value{}, err
		}
		vals["status"] = nested
	} else {
		vals["status"] = tftypes.NewValue(StackModelStatusType(), nil)
	}
	vals["workspace"] = tftypes.NewValue(tftypes.String, m.Workspace)
	return tftypes.NewValue(StackModelType(), vals), nil
}
func WorkspaceModelStatusType() tftypes.Type {
	return tftypes.Object{AttributeTypes: map[string]tftypes.Type{"phase": tftypes.String}}
}
func WorkspaceModelStatusFromValue(v tftypes.Value) (provider.WorkspaceModelStatus, error) {
	var m provider.WorkspaceModelStatus
	if v.IsNull() {
		return m, nil
	}
	if v.IsKnown() != true {
		return m, fmt.Errorf("cannot decode unknown %s value", "WorkspaceModelStatus")
	}
	var vals map[string]tftypes.Value
	if err := v.As(&vals); err != nil {
		return m, err
	}
	if val, ok := vals["phase"]; ok {
		v, err := decodeStringPtr(val)
		if err != nil {
			return m, err
		}
		m.Phase = v
	}
	return m, nil
}
func WorkspaceModelStatusToValue(m provider.WorkspaceModelStatus) (tftypes.Value, error) {
	vals := map[string]tftypes.Value{}
	if m.Phase != nil {
		vals["phase"] = tftypes.NewValue(tftypes.String, *m.Phase)
	} else {
		vals["phase"] = tftypes.NewValue(tftypes.String, nil)
	}
	return tftypes.NewValue(WorkspaceModelStatusType(), vals), nil
}
func WorkspaceModelType() tftypes.Type {
	return tftypes.Object{AttributeTypes: map[string]tftypes.Type{"api_version": tftypes.String, "kind": tftypes.String, "labels": tftypes.Map{ElementType: tftypes.String}, "name": tftypes.String, "status": WorkspaceModelStatusType()}}
}
func WorkspaceModelFromValue(v tftypes.Value) (provider.WorkspaceModel, error) {
	var m provider.WorkspaceModel
	if v.IsNull() {
		return m, nil
	}
	if v.IsKnown() != true {
		return m, fmt.Errorf("cannot decode unknown %s value", "WorkspaceModel")
	}
	var vals map[string]tftypes.Value
	if err := v.As(&vals); err != nil {
		return m, err
	}
	if val, ok := vals["api_version"]; ok {
		v, err := decodeStringPtr(val)
		if err != nil {
			return m, err
		}
		m.ApiVersion = v
	}
	if val, ok := vals["kind"]; ok {
		v, err := decodeStringPtr(val)
		if err != nil {
			return m, err
		}
		m.Kind = v
	}
	if val, ok := vals["labels"]; ok && !val.IsNull() {
		var elems map[string]tftypes.Value
		if err := val.As(&elems); err != nil {
			return m, err
		}
		m.Labels = make(map[string]string, len(elems))
		for k, ev := range elems {
			var tmp string
			if err := decodeString(ev, &tmp); err != nil {
				return m, err
			}
			m.Labels[k] = tmp
		}
	}
	if val, ok := vals["name"]; ok {
		if err := decodeString(val, &m.Name); err != nil {
			return m, err
		}
	} else {
		return m, fmt.Errorf("missing required attribute %q", "name")
	}
	if val, ok := vals["status"]; ok && !val.IsNull() {
		nested, err := WorkspaceModelStatusFromValue(val)
		if err != nil {
			return m, err
		}
		m.Status = &nested
	}
	return m, nil
}
func WorkspaceModelToValue(m provider.WorkspaceModel) (tftypes.Value, error) {
	vals := map[string]tftypes.Value{}
	if m.ApiVersion != nil {
		vals["api_version"] = tftypes.NewValue(tftypes.String, *m.ApiVersion)
	} else {
		vals["api_version"] = tftypes.NewValue(tftypes.String, nil)
	}
	if m.Kind != nil {
		vals["kind"] = tftypes.NewValue(tftypes.String, *m.Kind)
	} else {
		vals["kind"] = tftypes.NewValue(tftypes.String, nil)
	}
	if m.Labels != nil {
		elems := make(map[string]tftypes.Value, len(m.Labels))
		for k, v := range m.Labels {
			elems[k] = tftypes.NewValue(tftypes.String, v)
		}
		vals["labels"] = tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, elems)
	} else {
		vals["labels"] = tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, nil)
	}
	vals["name"] = tftypes.NewValue(tftypes.String, m.Name)
	if m.Status != nil {
		nested, err := WorkspaceModelStatusToValue(*m.Status)
		if err != nil {
			return tftypes.Value{}, err
		}
		vals["status"] = nested
	} else {
		vals["status"] = tftypes.NewValue(WorkspaceModelStatusType(), nil)
	}
	return tftypes.NewValue(WorkspaceModelType(), vals), nil
}
func CreatePullRequestModelType() tftypes.Type {
	return tftypes.Object{AttributeTypes: map[string]tftypes.Type{}}
}
func CreatePullRequestModelFromValue(v tftypes.Value) (provider.CreatePullRequestModel, error) {
	var m provider.CreatePullRequestModel
	if v.IsNull() {
		return m, nil
	}
	if v.IsKnown() != true {
		return m, fmt.Errorf("cannot decode unknown %s value", "CreatePullRequestModel")
	}
	var vals map[string]tftypes.Value
	if err := v.As(&vals); err != nil {
		return m, err
	}
	return m, nil
}
func CreatePullRequestModelToValue(m provider.CreatePullRequestModel) (tftypes.Value, error) {
	vals := map[string]tftypes.Value{}
	return tftypes.NewValue(CreatePullRequestModelType(), vals), nil
}
func UpdatePullRequestModelType() tftypes.Type {
	return tftypes.Object{AttributeTypes: map[string]tftypes.Type{}}
}
func UpdatePullRequestModelFromValue(v tftypes.Value) (provider.UpdatePullRequestModel, error) {
	var m provider.UpdatePullRequestModel
	if v.IsNull() {
		return m, nil
	}
	if v.IsKnown() != true {
		return m, fmt.Errorf("cannot decode unknown %s value", "UpdatePullRequestModel")
	}
	var vals map[string]tftypes.Value
	if err := v.As(&vals); err != nil {
		return m, err
	}
	return m, nil
}
func UpdatePullRequestModelToValue(m provider.UpdatePullRequestModel) (tftypes.Value, error) {
	vals := map[string]tftypes.Value{}
	return tftypes.NewValue(UpdatePullRequestModelType(), vals), nil
}
func CreateTaskModelType() tftypes.Type {
	return tftypes.Object{AttributeTypes: map[string]tftypes.Type{}}
}
func CreateTaskModelFromValue(v tftypes.Value) (provider.CreateTaskModel, error) {
	var m provider.CreateTaskModel
	if v.IsNull() {
		return m, nil
	}
	if v.IsKnown() != true {
		return m, fmt.Errorf("cannot decode unknown %s value", "CreateTaskModel")
	}
	var vals map[string]tftypes.Value
	if err := v.As(&vals); err != nil {
		return m, err
	}
	return m, nil
}
func CreateTaskModelToValue(m provider.CreateTaskModel) (tftypes.Value, error) {
	vals := map[string]tftypes.Value{}
	return tftypes.NewValue(CreateTaskModelType(), vals), nil
}
func UpdateTaskModelType() tftypes.Type {
	return tftypes.Object{AttributeTypes: map[string]tftypes.Type{}}
}
func UpdateTaskModelFromValue(v tftypes.Value) (provider.UpdateTaskModel, error) {
	var m provider.UpdateTaskModel
	if v.IsNull() {
		return m, nil
	}
	if v.IsKnown() != true {
		return m, fmt.Errorf("cannot decode unknown %s value", "UpdateTaskModel")
	}
	var vals map[string]tftypes.Value
	if err := v.As(&vals); err != nil {
		return m, err
	}
	return m, nil
}
func UpdateTaskModelToValue(m provider.UpdateTaskModel) (tftypes.Value, error) {
	vals := map[string]tftypes.Value{}
	return tftypes.NewValue(UpdateTaskModelType(), vals), nil
}
