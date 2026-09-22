// Package radio implements the radio-link (RRL) domain: link budget, RSSI,
// fade margin, capacity, ATPC, ACM and the radioLinkDown/radioLinkDegraded
// alarms.
//
// The domain is a deliberate simplification of a real radio stack. It reacts
// to events from internal/event and never imports the other domains.
package radio
