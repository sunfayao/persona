package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/sunfayao/persona/server/internal/apierr"
	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/nodeproto"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// spaceStateTool is the MCP tool a space track's conversations replace their SPACE.md's state block with.
const spaceStateTool = "space_state_update"

// errSpaceMDNode: the call did not reach a node that keeps SPACE.md (offline, or not announcing it).
var errSpaceMDNode = errors.New("the assistant's computer cannot be reached for SPACE.md")

// trackSpaceMDNode: machine's node reads and writes a track's SPACE.md (nodeproto.TrackSpaceMD).
func (s *Service) trackSpaceMDNode(machine string) bool {
	nodes, ok := s.physicalDevices().(nodeAnnouncer)
	return ok && nodes.NodeAnnounced(machine, nodeproto.TrackSpaceMD)
}

// spaceMD makes call c on the SPACE.md of space track slug, on the assistant's machine. The status says what went
// wrong when err is set: 404 no such track (or no directory of its own), 503 the machine's node cannot be asked.
func (s *Service) spaceMD(ctx context.Context, slug string, c assistantwire.SpaceMDCall) (assistantwire.SpaceMD, string, int, error) {
	e, ok := s.spaceTrack(ctx, slug)
	if !ok {
		return assistantwire.SpaceMD{}, "", http.StatusNotFound, fmt.Errorf("track %q has no SPACE.md", slug)
	}
	machine := s.settings(ctx).MachineID
	if machine == "" || s.isWorkMachine(machine) {
		return assistantwire.SpaceMD{}, "", http.StatusServiceUnavailable, errSpaceMDNode
	}
	if !s.trackSpaceMDNode(machine) {
		return assistantwire.SpaceMD{}, "", http.StatusServiceUnavailable, i18n.Err("assistant-spaces.node_no_track_spaces", i18n.Args{"machine": s.machineName(machine)})
	}
	c.Dir, c.Slug = trackRel(e), e.Slug
	var out assistantwire.SpaceMD
	err := s.call(ctx, machine, assistantwire.DeviceAssistantSpaceMD, c, &out)
	if err != nil {
		var re *rpc.Error
		if errors.Is(err, apierr.ErrOffline) || errors.Is(err, rpc.ErrClosed) || errors.Is(err, ErrNoDevice) ||
			errors.As(err, &re) && (re.Code == proto.CodeNodeOutdated || re.Code == assistantwire.CodeMachineOffline) {
			return assistantwire.SpaceMD{}, machine, http.StatusServiceUnavailable, errSpaceMDNode
		}
		return assistantwire.SpaceMD{}, machine, http.StatusBadGateway, err
	}
	return out, machine, http.StatusOK, nil
}

// spaceMDRefusal is the HTTP status and the words of a node's refusal (assistantwire.SpaceMD.Refused).
func spaceMDRefusal(why string) (int, error) {
	switch why {
	case assistantwire.SpaceMDManaged:
		return http.StatusBadRequest, errors.New("the blocks Persona keeps in SPACE.md (between persona:managed markers) and their markers stay as they are")
	case assistantwire.SpaceMDTooLarge:
		return http.StatusRequestEntityTooLarge, errors.New("SPACE.md is larger than 64 KB: edit it on the computer")
	case assistantwire.SpaceMDNoState:
		return http.StatusConflict, errors.New("SPACE.md has no state block (persona:managed:state)")
	case assistantwire.SpaceMDMissing:
		return http.StatusNotFound, errors.New("the track's directory or its SPACE.md is not there")
	}
	return http.StatusBadGateway, fmt.Errorf("SPACE.md: the computer refused (%s)", why)
}

// spaceTools are the tools conversation session on machine gets for being in a space track (spaceTrack): the one
// that replaces its SPACE.md's state block. None for any other conversation, the main chat's included, so its list stays as it was.
func (s *Service) spaceTools(ctx context.Context, machine, session string) []proto.Tool {
	if session == "" || !s.sameMachine(machine, s.settings(ctx).MachineID) {
		return nil
	}
	space, ok := s.selfSpace(ctx, session)
	if !ok || space == "" {
		return nil
	}
	if _, ok := s.spaceTrack(ctx, space); !ok {
		return nil
	}
	return toolsNamed([]string{spaceStateTool})
}

// toolSpaceStateUpdate replaces the state block of the SPACE.md of the space track conversation self is in. The list
// offers it only there (spaceTools); a call from anywhere else is refused here as well.
func (s *Service) toolSpaceStateUpdate(ctx context.Context, machine, self string, args json.RawMessage) (string, error) {
	_, guest, err := s.guestOf(ctx, machine, self)
	if err != nil || guest || s.isWorkMachine(machine) || isOutwardReader(ctx) || len(s.spaceTools(ctx, machine, self)) == 0 {
		return "", errors.New(spaceStateTool + " is only for the conversations inside a space")
	}
	var p struct {
		Text string `json:"text"`
	}
	if err := strictParams(spaceStateTool, args, &p); err != nil {
		return "", err
	}
	text := strings.TrimRight(p.Text, "\r\n")
	switch {
	case !utf8.ValidString(text):
		return "", errors.New("text: not UTF-8 text")
	case len(text) > assistantwire.SpaceMDStateMax:
		return "", fmt.Errorf("text is %d bytes; the state block holds at most %d: keep only the current state", len(text), assistantwire.SpaceMDStateMax)
	}
	space, _ := s.selfSpace(ctx, self)
	out, _, _, err := s.spaceMD(ctx, space, assistantwire.SpaceMDCall{Op: assistantwire.SpaceMDPutState, State: text})
	if err != nil {
		return "", err
	}
	switch out.Refused {
	case "":
	case assistantwire.SpaceMDManaged:
		return "", errors.New("text may not hold a persona:managed marker")
	case assistantwire.SpaceMDNoState:
		return "", errors.New("SPACE.md has no state block: the user took it out, so it is not written; tell the user if it matters")
	default:
		_, err := spaceMDRefusal(out.Refused)
		return "", err
	}
	// The instructions are written again from it at the next refresh (refreshInstructions), not in the middle of this
	// turn.
	return "The state block of SPACE.md now holds what you gave; the rest of SPACE.md is unchanged, and the previous file is kept in .space/history/.", nil
}
