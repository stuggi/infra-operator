package ipam

import (
	"fmt"
	"net"
	"net/netip"

	networkv1 "github.com/openstack-k8s-operators/infra-operator/apis/network/v1beta1"
	"golang.org/x/exp/slices"
)

// AssignIPDetails -
type AssignIPDetails struct {
	NetName     string
	SubNetName  string
	IPNet       net.IPNet
	Hostname    string
	Ranges      []networkv1.AllocationRange
	Reservelist *networkv1.ReservationList
	FixedIP     net.IP
}

// AssignIP assigns an IP using a range and a reserve list.
func (a *AssignIPDetails) AssignIP() (networkv1.IPAddress, error) {
	if a.FixedIP != nil {
		reservation, err := a.fixedIPExists()
		if err != nil {
			return networkv1.IPAddress{}, err
		}

		return *reservation, err
	}

	newIP, err := a.iterateForAssignment()
	if err != nil {
		return networkv1.IPAddress{}, err
	}

	return *newIP, nil
}

func (a *AssignIPDetails) fixedIPExists() (*networkv1.IPAddress, error) {
	fixedIP := a.FixedIP.String()
	for _, allocRange := range a.Ranges {
		if slices.Contains(allocRange.ExcludeAddresses, fixedIP) {
			return nil, fmt.Errorf("FixedIP %s is in ExcludeAddresses", fixedIP)
		}
	}

	// validate of nextip is already in a reservation and its not us
	f := func(c networkv1.Reservation) bool {
		return c.Spec.Reservation[a.NetName].Address == fixedIP
	}
	idx := slices.IndexFunc(a.Reservelist.Items, f)
	if idx >= 0 && string(*a.Reservelist.Items[idx].Spec.Hostname) != a.Hostname {
		return nil, fmt.Errorf(fmt.Sprintf("%s already reserved for %s", fixedIP, string(*a.Reservelist.Items[idx].Spec.Hostname)))
	}

	return &networkv1.IPAddress{Address: fixedIP}, nil
}

// IterateForAssignment iterates given an IP/IPNet and a list of reserved IPs
func (a *AssignIPDetails) iterateForAssignment() (*networkv1.IPAddress, error) {
	for _, allocRange := range a.Ranges {
		firstip, err := netip.ParseAddr(allocRange.Start)
		if err != nil {
			return nil, fmt.Errorf("failed to parse AllocationRange.Start IP %s: %w", allocRange.Start, err)
		}
		lastip, err := netip.ParseAddr(allocRange.End)
		if err != nil {
			return nil, fmt.Errorf("failed to parse AllocationRange.End IP %s: %w", allocRange.End, err)
		}

		// Iterate every IP address in the range
		nextip, _ := netip.ParseAddr(firstip.String())
		endip, _ := netip.ParseAddr(lastip.String())
		for nextip.Compare(endip) < 1 {
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

			// validate of nextip is already in a reservation and its not us
			f := func(c networkv1.Reservation) bool {
				return c.Spec.Reservation[a.NetName].Address == nextip.String()
			}
			idx := slices.IndexFunc(a.Reservelist.Items, f)
			if idx >= 0 && string(*a.Reservelist.Items[idx].Spec.Hostname) != a.Hostname {
				nextip = nextip.Next()
				continue
			}

			// Found a free IP
			return &networkv1.IPAddress{
				Network: networkv1.NetNameStr(a.NetName),
				Subnet:  networkv1.NetNameStr(a.SubNetName),
				Address: nextip.String(),
			}, nil
		}
	}

	return nil, fmt.Errorf(fmt.Sprintf("no ip address could be created for %s in subnet %s", a.Hostname, a.IPNet.String()))
}
