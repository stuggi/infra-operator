package ipam

import (
	"fmt"
	"net"
	"net/netip"

	networkv1 "github.com/openstack-k8s-operators/infra-operator/apis/network/v1beta1"
	helper "github.com/openstack-k8s-operators/lib-common/modules/common/helper"
	"golang.org/x/exp/slices"
	//"k8s.io/utils/strings/slices"
)

// AssignIPDetails -
type AssignIPDetails struct {
	IPNet       net.IPNet
	Hostname    string
	Ranges      []networkv1.AllocationRange
	Reservelist map[string]string
	FixedIP     net.IP
}

// AssignmentError defines an IP assignment error.
type AssignmentError struct {
	firstIP net.IP
	lastIP  net.IP
	ipnet   net.IPNet
}

func (a AssignmentError) Error() string {
	return fmt.Sprintf("Could not allocate IP in range: ip: %v / - %v / range: %#v", a.firstIP, a.lastIP, a.ipnet)
}

// AssignIP assigns an IP using a range and a reserve list.
func (a *AssignIPDetails) AssignIP(
	helper *helper.Helper,
) (networkv1.IPAddress, error) {
	if a.FixedIP != nil {
		exists, reservation, err := a.fixedIPExists()
		if exists {
			return reservation, nil
		}
		return networkv1.IPAddress{}, err
	}

	newIP, err := a.iterateForAssignment(helper)
	if err != nil {
		return networkv1.IPAddress{}, err
	}

	return newIP, nil
}

func (a *AssignIPDetails) fixedIPExists() (bool, networkv1.IPAddress, error) {
	fixedIPStr := a.FixedIP.String()
	fixedIP := netip.MustParseAddr(fixedIPStr)
	for _, allocRange := range a.Ranges {
		firstip := netip.MustParseAddr(allocRange.Start)
		lastip := netip.MustParseAddr(allocRange.End)

		// The result will be 0 if ip == ip2, -1 if ip < ip2, and +1 if ip > ip2.
		if (fixedIP.Compare(firstip) >= 0) && (fixedIP.Compare(lastip) <= 0) &&
			slices.Contains(allocRange.ExcludeAddresses, fixedIP.String()) {

			if hostname, exist := a.Reservelist[fixedIPStr]; exist && hostname != a.Hostname {
				return false, networkv1.IPAddress{}, fmt.Errorf(fmt.Sprintf("%s already reserved for %s", fixedIPStr, hostname))
			}

			return true, networkv1.IPAddress{Address: fixedIPStr}, nil
		}
	}
	return false, networkv1.IPAddress{}, fmt.Errorf("Does not exist in range %s", fixedIPStr)
}

// IterateForAssignment iterates given an IP/IPNet and a list of reserved IPs
func (a *AssignIPDetails) iterateForAssignment(
	helper *helper.Helper,
) (networkv1.IPAddress, error) {
	for _, allocRange := range a.Ranges {
		firstip := netip.MustParseAddr(allocRange.Start)
		lastip := netip.MustParseAddr(allocRange.End)

		// Iterate every IP address in the range
		nextip, _ := netip.ParseAddr(firstip.String())
		endip, _ := netip.ParseAddr(lastip.String())
		for nextip.Less(endip) {
			// Skip addresses ending with 0
			ipSlice := nextip.AsSlice()
			if (nextip.Is4()) && (ipSlice[3] == 0) || (nextip.Is6() && (ipSlice[14] == 0) && (ipSlice[15] == 0)) {
				nextip = nextip.Next()
				continue
			}

			// Skip if in ExcludeAddresses list
			if slices.Contains(allocRange.ExcludeAddresses, nextip.String()) {
				nextip = nextip.Next()
				continue
			}

			if hostname, exist := a.Reservelist[nextip.String()]; exist && hostname != a.Hostname {
				helper.GetLogger().Info(fmt.Sprintf("%s already reserved for %s", nextip.String(), hostname))
				nextip = nextip.Next()
				continue
			}

			// Found a free IP
			return networkv1.IPAddress{Address: nextip.String()}, nil
		}
	}

	return networkv1.IPAddress{}, fmt.Errorf(fmt.Sprintf("no ip address could be created for %s in subnet %s", a.Hostname, a.IPNet.String()))
}

// GetCidrParts - returns addr and cidr suffix
func GetCidrParts(cidr string) (string, int, error) {
	ipAddr, net, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", 0, err
	}

	cidrSuffix, _ := net.Mask.Size()

	return ipAddr.String(), cidrSuffix, nil
}
