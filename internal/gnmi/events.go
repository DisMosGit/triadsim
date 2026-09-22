package gnmi

import (
	"strings"

	"github.com/DisMosGit/triadsim/internal/event"
)

// eventResourceRoot is the resource name a configuration change carries; it
// selects the root path, so every subscription re-reads its own subtree.
const eventResourceRoot = "device"

// affectedPaths returns the model paths an event changed, or nil when the event
// cannot be mapped onto the model.
//
// Domain events name their resource in the domain's own vocabulary (a radio
// link name, "ptp/clock", "l2/stp/<port>"); this is the one place that
// translates them into router paths, so a domain keeps publishing without
// knowing about gNMI.
func affectedPaths(e event.Event) []string {
	switch {
	case e.Type == event.TypeConfigChanged:
		return []string{""}
	case e.Resource == "":
		return nil
	case e.Resource == "ptp/clock":
		return []string{"ptp/clock"}
	case strings.HasPrefix(e.Resource, "l2/stp/"):
		port := strings.TrimPrefix(e.Resource, "l2/stp/")
		return portPath(port, "stp/state/ports/port")
	case strings.HasPrefix(e.Resource, "l2/storm/"):
		port := strings.TrimPrefix(e.Resource, "l2/storm/")
		return portPath(port, "interfaces/interface")
	default:
		// A radio link publishes its own name as the resource.
		if !plainName(e.Resource) {
			return nil
		}
		return []string{"interfaces/interface[name=" + e.Resource + "]/radio-link"}
	}
}

// portPath renders one interface-indexed list entry, and nothing when the port
// name is not a plain identifier.
func portPath(port, list string) []string {
	if !plainName(port) {
		return nil
	}
	return []string{list + "[name=" + port + "]"}
}

// plainName reports whether text can be used inside a list key predicate.
func plainName(text string) bool {
	return text != "" && !strings.ContainsAny(text, "/[]=")
}

// matchesSubscription reports whether an affected path concerns a subscription
// path: either path lies on the other one. The root subscription matches every
// affected path.
func matchesSubscription(subscription, affected string) bool {
	if affected == "" {
		return true
	}
	return pathsOverlap(subscription, affected)
}

// notificationTarget is the path a subscription is notified about: the
// subscription itself, or the affected path when the subscription is the model
// root, so a root subscription does not read the whole device on every event.
func notificationTarget(subscription, affected string) string {
	if subscription == "" {
		return affected
	}
	return subscription
}
