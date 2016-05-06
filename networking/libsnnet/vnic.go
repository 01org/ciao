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
	"net"
	"strings"
	"syscall"

	"github.com/vishvananda/netlink"
)

// NewVnic is used to initialize the Vnic properties
// This has to be called prior to Create() or GetDevice()
func newVnic(id string) (*Vnic, error) {
	Vnic := &Vnic{}
	Vnic.Link = &netlink.GenericLink{}
	Vnic.GlobalID = id
	Vnic.Role = TenantVM
	return Vnic, nil
}

// NewContainerVnic is used to initialize a container Vnic properties
// This has to be called prior to Create() or GetDevice()
func newContainerVnic(id string) (*Vnic, error) {
	Vnic := &Vnic{}
	Vnic.Link = &netlink.Veth{}
	Vnic.GlobalID = id
	Vnic.Role = TenantContainer
	return Vnic, nil
}

//InterfaceName is used to retrieve the name of the physical interface to
//which the VM or the container needs to be connected to
//Returns "" if the link is not setup
func (v *Vnic) interfaceName() string {
	switch v.Role {
	case TenantVM:
		return v.LinkName
	case TenantContainer:
		return v.peerName()
	default:
		return ""
	}
}

//PeerName is used to retrieve the peer name
//Returns "" if the link is not setup or if the link
//has no peer
func (v *Vnic) peerName() string {
	if v.Role != TenantContainer {
		return v.LinkName
	}

	if strings.HasPrefix(v.LinkName, prefixVnicHost) {
		return strings.Replace(v.LinkName, prefixVnicHost, prefixVnicCont, 1)
	}
	if strings.HasPrefix(v.LinkName, prefixVnicCont) {
		return strings.Replace(v.LinkName, prefixVnicCont, prefixVnicHost, 1)
	}
	return ""
}

// GetDevice is used to associate with an existing VNIC provided it satisfies
// the needs of a Vnic. Returns error if the VNIC does not exist
func (v *Vnic) getDevice() error {

	if v.GlobalID == "" {
		return netError(v, "get device unnamed vnic")
	}

	link, err := netlink.LinkByAlias(v.GlobalID)
	if err != nil {
		return netError(v, "get device interface does not exist: %v", v.GlobalID)
	}

	switch v.Role {
	case TenantVM:
		vl, ok := link.(*netlink.GenericLink)
		if !ok {
			return netError(v, "get device incorrect interface type %v %v", v.GlobalID, link.Type())
		}

		// TODO: Why do both tun and tap interfaces return the type tun
		if link.Type() != "tun" {
			return netError(v, "get device incorrect interface type %v %v", v.GlobalID, link.Type())
		}

		if flags := uint(link.Attrs().Flags); (flags & syscall.IFF_TAP) == 0 {
			return netError(v, "get device incorrect interface type %v %v", v.GlobalID, link)
		}
		v.LinkName = vl.Name
		v.Link = vl
	case TenantContainer:
		vl, ok := link.(*netlink.Veth)
		if !ok {
			return netError(v, "get device incorrect interface type %v %v", v.GlobalID, link.Type())
		}
		v.LinkName = vl.Name
		v.Link = vl
	default:
		return netError(v, " invalid or unsupported VNIC type %v", v.GlobalID)
	}

	return nil
}

// Create instantiates new VNIC
func (v *Vnic) create() error {
	var err error

	if v.GlobalID == "" {
		return netError(v, "create cannot create an unnamed vnic")
	}

	switch v.Role {
	case TenantVM:
	case TenantContainer:
	default:
		return netError(v, "invalid vnic role specified")
	}

	if v.LinkName == "" {
		if v.LinkName, err = genIface(v, true); err != nil {
			return netError(v, "create geniface %v %v", v.GlobalID, err)
		}

		if _, err := netlink.LinkByAlias(v.GlobalID); err == nil {
			return netError(v, "create interface exists %v", v.GlobalID)
		}
	}

	switch v.Role {
	case TenantVM:

		tap := &netlink.Tuntap{
			LinkAttrs: netlink.LinkAttrs{Name: v.LinkName},
			Mode:      netlink.TUNTAP_MODE_TAP,
		}

		if err := netlink.LinkAdd(tap); err != nil {
			return netError(v, "create link add %v %v", v.GlobalID, err)
		}

		link, err := netlink.LinkByName(v.LinkName)
		if err != nil {
			return netError(v, "create link by name %v %v", v.GlobalID, err)
		}

		vl, ok := link.(*netlink.GenericLink)
		if !ok {
			return netError(v, "create incorrect interface type %v %v", v.GlobalID, link.Type())
		}

		v.Link = vl
	case TenantContainer:
		//We create only the host side veth, the container side is setup by the kernel
		veth := &netlink.Veth{
			LinkAttrs: netlink.LinkAttrs{
				Name: v.LinkName,
			},
			PeerName: v.peerName(),
		}

		if err := netlink.LinkAdd(veth); err != nil {
			return netError(v, "create link add %v %v", v.GlobalID, err)
		}

		link, err := netlink.LinkByName(v.LinkName)
		if err != nil {
			return netError(v, "create link by name %v %v", v.GlobalID, err)
		}
		vl, ok := link.(*netlink.Veth)
		if !ok {
			return netError(v, "create incorrect interface type %v %v", v.GlobalID, link.Type())
		}

		v.Link = vl
	}

	if err := v.setAlias(v.GlobalID); err != nil {
		_ = v.destroy()
		return netError(v, "create set alias %v %v", v.GlobalID, err)
	}

	return nil
}

// Destroy a VNIC
func (v *Vnic) destroy() error {

	if v.Link == nil || v.Link.Attrs().Index == 0 {
		return netError(v, "destroy unnitialized")
	}

	if err := netlink.LinkDel(v.Link); err != nil {
		return netError(v, "destroy link [%v] del [%v]", v.LinkName, err)
	}

	return nil

}

// Attach the VNIC to a bridge or a switch. Will return error if the VNIC
// incapable of binding to the specified device
func (v *Vnic) attach(dev interface{}) error {

	if v.Link == nil || v.Link.Attrs().Index == 0 {
		return netError(v, "attach unnitialized")
	}

	br, ok := dev.(*Bridge)
	if !ok {
		return netError(v, "attach device %v, %T", dev, dev)
	}

	if br.Link == nil || br.Link.Index == 0 {
		return netError(v, "attach bridge unnitialized")
	}

	if err := netlink.LinkSetMaster(v.Link, br.Link); err != nil {
		return netError(v, "attach set master %v", err)
	}

	return nil
}

// Detach the VNIC from the device it is attached to
func (v *Vnic) detach(dev interface{}) error {

	if v.Link == nil || v.Link.Attrs().Index == 0 {
		return netError(v, "detach unnitialized")
	}

	br, ok := dev.(*Bridge)

	if !ok {
		return netError(v, "detach unknown device %v, %T", dev, dev)
	}

	if br.Link == nil {
		return netError(v, "detach bridge unnitialized")
	}

	if err := netlink.LinkSetNoMaster(v.Link); err != nil {
		return netError(v, "detach set no master %v", err)
	}

	return nil
}

// Enable the VNIC
func (v *Vnic) enable() error {

	if v.Link == nil || v.Link.Attrs().Index == 0 {
		return netError(v, "enable unnitialized")
	}

	if err := netlink.LinkSetUp(v.Link); err != nil {
		return netError(v, "enable link set set up %v", err)
	}

	return nil

}

// Disable the VNIC
func (v *Vnic) disable() error {

	if v.Link == nil || v.Link.Attrs().Index == 0 {
		return netError(v, "disable unnitialized")
	}

	if err := netlink.LinkSetDown(v.Link); err != nil {
		return netError(v, "disable link set down %v", err)
	}

	return nil
}

//SetMTU of the interface
func (v *Vnic) setMTU(mtu int) error {

	if v.Link == nil || v.Link.Attrs().Index == 0 {
		return netError(v, "disable unnitialized")
	}

	switch v.Role {
	case TenantVM:
		/* Set by DHCP. */
	case TenantContainer:
		/* Need to set the MTU of both ends */
		if err := netlink.LinkSetMTU(v.Link, mtu); err != nil {
			return netError(v, "link set mtu %v", err)
		}
		peerVeth := &netlink.Veth{
			LinkAttrs: netlink.LinkAttrs{
				Name: v.peerName(),
			},
			PeerName: v.LinkName,
		}
		if err := netlink.LinkSetMTU(peerVeth, mtu); err != nil {
			return netError(v, "link set peer mtu %v", err)
		}
	}

	return nil
}

//SetHardwareAddr of the interface
func (v *Vnic) setHardwareAddr(addr net.HardwareAddr) error {

	if v.Link == nil || v.Link.Attrs().Index == 0 {
		return netError(v, "disable unnitialized")
	}

	switch v.Role {
	case TenantVM:
		/* Set by QEMU. */
	case TenantContainer:
		/* Need to set the MAC on the container side */
		peerVeth := &netlink.Veth{
			LinkAttrs: netlink.LinkAttrs{
				Name: v.peerName(),
			},
			PeerName: v.LinkName,
		}
		if err := netlink.LinkSetHardwareAddr(peerVeth, addr); err != nil {
			return netError(v, "link set peer mtu %v", err)
		}
	}

	return nil
}

func (v *Vnic) setAlias(alias string) error {

	if v.Link == nil || v.Link.Attrs().Index == 0 {
		return netError(v, "set alias unnitialized")
	}

	if err := netlink.LinkSetAlias(v.Link, alias); err != nil {
		return netError(v, "link set alias %v %v", alias, err)
	}

	return nil
}
