package render

import (
	"bytes"
	"fmt"
	"strings"
)

// LobbyRenderer tracks lobby panel state for incremental updates.
// First Refresh call: full LobbyScreen (borders, headers, everything).
// Subsequent calls: only rows that changed in the rooms or leaderboard panels.
// Chat/input are always refreshed since fullRefresh drains the UpdateCh.
type LobbyRenderer struct {
	prevRooms        []RoomInfo
	prevLeaders      []LeaderEntry
	prevNoRooms      bool
	prevSelectedRoom int
	prevOnline       int
	ascii            bool
	first            bool
}

func NewLobbyRenderer(ascii bool) *LobbyRenderer {
	return &LobbyRenderer{ascii: ascii, first: true, prevSelectedRoom: -1}
}

// Reset forces the next Refresh call to do a full redraw (e.g. after returning from a game).
func (lr *LobbyRenderer) Reset() {
	lr.first = true
}

// Refresh returns the minimal byte sequence needed to update the lobby screen.
func (lr *LobbyRenderer) Refresh(playerName string, rooms []RoomInfo, leaders []LeaderEntry, chat []ChatMessage, inputBuf []byte, chatMode bool, selectedRoom, onlineCount int) []byte {
	if lr.first {
		lr.first = false
		lr.prevRooms = cloneRooms(rooms)
		lr.prevLeaders = cloneLeaders(leaders)
		lr.prevNoRooms = len(rooms) == 0
		lr.prevSelectedRoom = selectedRoom
		lr.prevOnline = onlineCount
		return LobbyScreen(playerName, rooms, leaders, chat, inputBuf, chatMode, lr.ascii, selectedRoom, onlineCount)
	}

	var buf bytes.Buffer
	noRooms := len(rooms) == 0

	// Rooms panel: rows 6-15, cols 2-40.
	// Force-redraw rows affected by selection change.
	selChanged := selectedRoom != lr.prevSelectedRoom
	for i := 0; i < 10; i++ {
		row := 6 + i
		var newR, oldR *RoomInfo
		if i < len(rooms) {
			r := rooms[i]
			newR = &r
		}
		if i < len(lr.prevRooms) {
			r := lr.prevRooms[i]
			oldR = &r
		}
		selRow := selChanged && (i == selectedRoom || i == lr.prevSelectedRoom)
		if roomInfoEqual(newR, oldR) && !selRow {
			continue
		}
		buf.WriteString(at(row, 2) + strings.Repeat(" ", 38))
		if newR != nil {
			buf.WriteString(at(row, 2) + RoomRow(*newR, i == selectedRoom))
		}
	}
	lr.prevSelectedRoom = selectedRoom

	// "No rooms yet." — only write or clear when state flips.
	if lr.prevNoRooms != noRooms {
		if noRooms {
			buf.WriteString(at(8, 4) + gray + "No rooms yet." + reset)
		} else {
			buf.WriteString(at(8, 4) + strings.Repeat(" ", 15))
		}
	}
	lr.prevNoRooms = noRooms

	// Leaderboard panel: rows 6-15, cols 43-79.
	for i := 0; i < 10; i++ {
		row := 6 + i
		var newL, oldL *LeaderEntry
		if i < len(leaders) {
			l := leaders[i]
			newL = &l
		}
		if i < len(lr.prevLeaders) {
			l := lr.prevLeaders[i]
			oldL = &l
		}
		if leaderEqual(newL, oldL) {
			continue
		}
		buf.WriteString(at(row, 43) + strings.Repeat(" ", 36))
		if newL != nil {
			buf.WriteString(at(row, 43) + fmt.Sprintf("%2d  %-10s %5d %4d %4d %4d",
				newL.Rank, truncate(newL.Name, 10), newL.Score, newL.Wins, newL.Kills, newL.WallsDestroyed))
		}
	}

	lr.prevRooms = cloneRooms(rooms)
	lr.prevLeaders = cloneLeaders(leaders)

	if onlineCount != lr.prevOnline {
		buf.WriteString(LobbyOnlineLine(onlineCount))
		lr.prevOnline = onlineCount
	}

	// Always resend chat section: fullRefresh drains UpdateCh, so this is the
	// only place a message that arrived during auto-refresh gets displayed.
	buf.Write(lobbyChatSection(chat, inputBuf, chatMode, lr.ascii))
	buf.WriteString(lobbyCursorPark(inputBuf, chatMode))

	return buf.Bytes()
}

// SelectionUpdate emits the minimal bytes to move the cursor highlight between rows.
func (lr *LobbyRenderer) SelectionUpdate(rooms []RoomInfo, selectedRoom int) []byte {
	var buf bytes.Buffer
	for _, i := range []int{lr.prevSelectedRoom, selectedRoom} {
		if i < 0 || i >= 10 {
			continue
		}
		buf.WriteString(at(6+i, 2) + strings.Repeat(" ", 38))
		if i < len(rooms) {
			buf.WriteString(at(6+i, 2) + RoomRow(rooms[i], i == selectedRoom))
		}
	}
	lr.prevSelectedRoom = selectedRoom
	return buf.Bytes()
}

func roomInfoEqual(a, b *RoomInfo) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func leaderEqual(a, b *LeaderEntry) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func cloneRooms(rooms []RoomInfo) []RoomInfo {
	out := make([]RoomInfo, len(rooms))
	copy(out, rooms)
	return out
}

func cloneLeaders(leaders []LeaderEntry) []LeaderEntry {
	out := make([]LeaderEntry, len(leaders))
	copy(out, leaders)
	return out
}
