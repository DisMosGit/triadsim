package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/DisMosGit/triadsim/internal/radio"
)

// alarmInjection is what one --type injects: the simulation endpoint it calls
// and the fade depth it asks for by default (0 leaves the choice to the
// domain).
type alarmInjection struct {
	endpoint string
	fadeDB   float64
}

// alarmInjections maps the --type values of alarm inject onto the simulation
// API.
var alarmInjections = map[string]alarmInjection{
	"radioLinkDown":     {endpoint: "/api/simulate/radio-failure"},
	"radioLinkDegraded": {endpoint: "/api/simulate/radio-failure", fadeDB: radio.DefaultDegradeFadeDB},
	"radioLinkUp":       {endpoint: "/api/simulate/radio-restore"},
}

// newAlarmCmd builds the alarm command group.
func newAlarmCmd(out io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alarm",
		Short: "Simulate device alarms",
		Long: "Alarm drives the simulation API of a running simulator: it posts the injected\n" +
			"condition over RESTCONF, which raises the alarm on the event bus and lets the\n" +
			"SNMP trap, the NETCONF notification and the Prometheus counter follow.",
		Args: cobra.NoArgs,
	}
	cmd.AddCommand(newAlarmInjectCmd(out))
	return cmd
}

// newAlarmInjectCmd builds the alarm inject command.
func newAlarmInjectCmd(out io.Writer) *cobra.Command {
	var alarmType, link, addr string
	var fadeDB float64

	cmd := &cobra.Command{
		Use:   "inject",
		Short: "Inject an alarm into a running simulator",
		Long: "Inject posts one simulated alarm to a running simulator. radioLinkDown fails\n" +
			"the link, radioLinkDegraded fades it into the degraded band and radioLinkUp\n" +
			"restores it; the radio domain then raises or clears the matching alarm.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAlarmInject(cmd.Context(), out, addr, alarmType, link, fadeDB, cmd.Flags().Changed("fade-db"))
		},
	}

	cmd.Flags().StringVar(&alarmType, "type", "radioLinkDown", "alarm to inject: "+strings.Join(alarmTypes(), ", "))
	cmd.Flags().StringVar(&link, "link", "", "radio link name; empty selects the first radio link")
	cmd.Flags().Float64Var(&fadeDB, "fade-db", 0, "injected fade depth in dB; 0 keeps the type's default")
	cmd.Flags().StringVar(&addr, "addr", DefaultSimulatorAddr, "base URL of the running simulator")

	return cmd
}

// runAlarmInject drives the simulation endpoint of one alarm type.
func runAlarmInject(ctx context.Context, out io.Writer, addr, alarmType, link string, fadeDB float64, fadeSet bool) error {
	injection, ok := alarmInjections[alarmType]
	if !ok {
		return fmt.Errorf("unknown alarm type %q (want %s)", alarmType, strings.Join(alarmTypes(), ", "))
	}
	if !fadeSet {
		fadeDB = injection.fadeDB
	}

	payload := map[string]any{"link": link}
	if injection.endpoint == "/api/simulate/radio-failure" {
		payload["fade-db"] = fadeDB
	}

	body, err := postJSON(ctx, endpoint(addr, injection.endpoint), payload)
	if err != nil {
		return fmt.Errorf("inject %s: %w", alarmType, err)
	}
	_, err = fmt.Fprintln(out, strings.TrimSpace(body))
	return err
}

// alarmTypes returns the accepted --type values in a stable order.
func alarmTypes() []string {
	names := make([]string, 0, len(alarmInjections))
	for name := range alarmInjections {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
