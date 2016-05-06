//
// Copyright (c) 2016 Intel Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package libsnnet

import (
	"strings"
	"testing"
)

//Test all Bridge primitives
//
//Tests creation, attach, enable, disable and destroy
//of a bridge interface. Any failure indicates a problem
//with the netlink library or kernel API
//
//Test is expected to pass
func TestBridge_Basic(t *testing.T) {

	bridge, _ := newBridge("go_testbr")

	if err := bridge.create(); err != nil {
		t.Errorf("Bridge creation failed: %v", err)
	}

	bridge1, _ := newBridge("go_testbr")

	if err := bridge1.getDevice(); err != nil {
		t.Errorf("Bridge Get Device failed: %v", err)
	}

	if err := bridge.enable(); err != nil {
		t.Errorf("Bridge enable failed: %v", err)
	}

	if err := bridge.disable(); err != nil {
		t.Errorf("Bridge enable failed: %v", err)
	}

	if err := bridge.destroy(); err != nil {
		t.Errorf("Bridge deletion failed: %v", err)
	}

	if err := bridge.destroy(); err == nil {
		t.Errorf("Bridge deletion should have failed")
	}

}

//Duplicate bridge detection
//
//Checks that duplicate bridge creation is handled
//gracefully and correctly
//
//Test is expected to pass
func TestBridge_Dup(t *testing.T) {
	bridge, _ := newBridge("go_testbr")

	if err := bridge.create(); err != nil {
		t.Errorf("Bridge creation failed: %v", err)
	}

	defer func() { _ = bridge.destroy() }()

	bridge1, _ := newBridge("go_testbr")
	if err := bridge1.create(); err == nil {
		t.Errorf("Duplicate Bridge creation: %v", err)
	}

}

//Negative test cases for bridge primitives
//
//Checks various negative test scenarios are gracefully
//handled
//
//Test is expected to pass
func TestBridge_Invalid(t *testing.T) {
	bridge, err := newBridge("go_testbr")

	if err = bridge.getDevice(); err == nil {
		t.Errorf("Non existing bridge: %v", bridge)
	}

	if !strings.HasPrefix(err.Error(), "bridge error") {
		t.Errorf("Invalid error format %v", err)
	}

	if err = bridge.destroy(); err == nil {
		t.Errorf("Uninitialized call: %v", err)
	}

	if !strings.HasPrefix(err.Error(), "bridge error") {
		t.Errorf("Invalid error format %v", err)
	}

	if err = bridge.enable(); err == nil {
		t.Errorf("Uninitialized call: %v", err)
	}

	if !strings.HasPrefix(err.Error(), "bridge error") {
		t.Errorf("Invalid error format %v", err)
	}

	if err = bridge.disable(); err == nil {
		t.Errorf("Uninitialized call: %v", err)
	}

	if !strings.HasPrefix(err.Error(), "bridge error") {
		t.Errorf("Invalid error format %v", err)
	}
}

//Tests attaching to an existing bridge
//
//Tests that you can attach to an existing bridge
//and perform all bridge operation on such a bridge
//
//Test is expected to pass
func TestBridge_GetDevice(t *testing.T) {
	bridge, _ := newBridge("go_testbr")

	if err := bridge.create(); err != nil {
		t.Errorf("Bridge creation failed: %v", err)
	}

	bridge1, _ := newBridge("go_testbr")

	if err := bridge1.getDevice(); err != nil {
		t.Errorf("Bridge Get Device failed: %v", err)
	}

	if err := bridge1.enable(); err != nil {
		t.Errorf("Uninitialized call: %v", err)
	}

	if err := bridge1.disable(); err != nil {
		t.Errorf("Uninitialized call: %v", err)
	}

	if err := bridge1.destroy(); err != nil {
		t.Errorf("Bridge destroy failed: %v", err)
	}
}
