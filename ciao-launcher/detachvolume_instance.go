/*
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
*/

package main

import (
	"github.com/01org/ciao/payloads"
	"github.com/golang/glog"
)

func processDetachVolume(vm virtualizer, cfg *vmConfig, instance, instanceDir, volumeUUID string, conn serverConn) *detachVolumeError {
	if _, found := cfg.Volumes[volumeUUID]; !found {
		detachErr := &detachVolumeError{nil, payloads.DetachVolumeNotAttached}
		glog.Errorf("%s not attached to attach instance %s [%s]",
			volumeUUID, instance, string(detachErr.code))
		return detachErr
	}

	delete(cfg.Volumes, volumeUUID)

	err := cfg.save(instanceDir)
	if err != nil {
		cfg.Volumes[volumeUUID] = struct{}{}
		detachErr := &detachVolumeError{err, payloads.DetachVolumeDetachFailure}
		glog.Errorf("Unable to persist instance %s state [%s]: %v",
			instance, string(detachErr.code), err)
		return detachErr
	}

	return nil
}
