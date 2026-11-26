// Package conformance holds a channel program to the channel extension point's protocol,
// after the plugin protocol's own checks (plugin/conformance): every channel the manifest contributes answers
// channel.send (sent, or cannot_send while there is nowhere to send to) the same way for the same ref, takes
// channel.typing, refuses a channel it does not have and a method it does not know; what both sides send holds to
// schema/extension/channel.schema.json.
package conformance
