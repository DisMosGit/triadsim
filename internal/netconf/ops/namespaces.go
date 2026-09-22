package ops

import "github.com/DisMosGit/triadsim/internal/router"

// namespaceFor returns the module namespace of a model path. The mapping lives
// in the router so NETCONF and RESTCONF qualify the same node consistently.
func namespaceFor(path string) string {
	return router.ModuleFor(path).Namespace
}
