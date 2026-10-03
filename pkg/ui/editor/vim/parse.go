package vim

// command is a parsed NORMAL or VISUAL mode command.
type command struct {
	count int    // product of the counts given; 0 when none
	op    string // pending operator ("d", "c", "y", ">", "<") or ""
	name  string // motion, text object ("iw"), command, or "line" for dd/cc/yy
	arg   string // character argument of f/F/t/T/r
	keys  []Key  // keys after the leading count (for dot repeat)
}

type parseStatus int

const (
	parseMore parseStatus = iota
	parseDone
	parseInvalid
)

// keyAliases maps special keys to the vim commands they act as.
var keyAliases = map[string]string{
	"left": "h", "right": "l", "up": "k", "down": "j",
	"home": "0", "end": "$", "backspace": "h", "space": "l", " ": "l",
	"delete": "x",
}

var simpleMotions = map[string]bool{
	"h": true, "j": true, "k": true, "l": true,
	"w": true, "b": true, "e": true, "W": true, "B": true, "E": true,
	"0": true, "^": true, "$": true, "G": true, ";": true, ",": true,
	"%": true, "{": true, "}": true, "_": true,
}

var argMotions = map[string]bool{"f": true, "F": true, "t": true, "T": true}

var operators = map[string]bool{"d": true, "c": true, "y": true, ">": true, "<": true}

var normalCommands = map[string]bool{
	"x": true, "X": true, "p": true, "P": true, "u": true, "ctrl+r": true, ".": true,
	"i": true, "a": true, "I": true, "A": true, "o": true, "O": true,
	"s": true, "S": true, "C": true, "D": true, "Y": true, "J": true, "~": true,
	"v": true, "V": true, "R": true, "/": true,
}

var visualCommands = map[string]bool{
	"x": true, "X": true, "d": true, "D": true, "c": true, "C": true, "s": true, "S": true,
	"y": true, "Y": true, ">": true, "<": true, "~": true, "J": true, "p": true, "P": true,
	"o": true, "O": true, "v": true, "V": true, "u": true, "U": true, "R": true, "I": true, "A": true,
}

var textObjects = map[string]bool{
	"w": true, "W": true, "\"": true, "'": true, "`": true,
	"(": true, ")": true, "b": true, "[": true, "]": true,
	"{": true, "}": true, "B": true, "<": true, ">": true, "p": true,
}

func keyIDs(keys []Key) []string {
	ids := make([]string, len(keys))
	for i, k := range keys {
		id := k.ID()
		if a, ok := keyAliases[id]; ok {
			id = a
		}
		ids[i] = id
	}
	return ids
}

// readCount reads a count starting at i. A leading "0" is a motion.
func readCount(ids []string, i int) (int, int) {
	n := 0
	for i < len(ids) {
		id := ids[i]
		if len(id) != 1 || id[0] < '0' || id[0] > '9' || (n == 0 && id == "0") {
			break
		}
		n = n*10 + int(id[0]-'0')
		if n > 99999 {
			n = 99999
		}
		i++
	}
	return n, i
}

func mulCount(a, b int) int {
	switch {
	case a == 0:
		return b
	case b == 0:
		return a
	}
	return min(a*b, 99999)
}

// parse parses the pending keys of a NORMAL or VISUAL mode command.
func parse(keys []Key, visual bool) (command, parseStatus) {
	ids := keyIDs(keys)
	var cmd command
	c1, i := readCount(ids, 0)
	if i == len(ids) {
		return cmd, parseMore
	}
	start := i
	cmd.count = c1
	k := ids[i]
	if k == "esc" {
		return cmd, parseInvalid
	}
	done := func(c command, end int) (command, parseStatus) {
		c.keys = keys[start:end]
		return c, parseDone
	}

	if visual {
		switch {
		case k == "i" || k == "a":
			if i+1 == len(ids) {
				return cmd, parseMore
			}
			if textObjects[ids[i+1]] {
				cmd.name = k + ids[i+1]
				return done(cmd, i+2)
			}
			if k == "a" || k == "i" {
				// "A"/"I" handle append/insert; lone a/i are invalid here.
				return cmd, parseInvalid
			}
		case k == "r":
			if i+1 == len(ids) {
				return cmd, parseMore
			}
			cmd.name, cmd.arg = "r", keys[i+1].Text
			if cmd.arg == "" {
				return cmd, parseInvalid
			}
			return done(cmd, i+2)
		case visualCommands[k]:
			cmd.name = k
			return done(cmd, i+1)
		}
		return parseMotion(cmd, ids, keys, i, start)
	}

	switch {
	case operators[k]:
		cmd.op = k
		i++
		c2, j := readCount(ids, i)
		cmd.count = mulCount(c1, c2)
		if j == len(ids) {
			return cmd, parseMore
		}
		k2 := ids[j]
		opKey := keys[i-1 : i]
		withOp := func(c command, end int) (command, parseStatus) {
			c.keys = append(append([]Key(nil), opKey...), keys[j:end]...)
			return c, parseDone
		}
		switch {
		case k2 == k:
			cmd.name = "line"
			return withOp(cmd, j+1)
		case k2 == "i" || k2 == "a":
			if j+1 == len(ids) {
				return cmd, parseMore
			}
			if !textObjects[ids[j+1]] {
				return cmd, parseInvalid
			}
			cmd.name = k2 + ids[j+1]
			return withOp(cmd, j+2)
		}
		c, st := parseMotion(cmd, ids, keys, j, j)
		if st == parseDone {
			c.keys = append(append([]Key(nil), opKey...), c.keys...)
		}
		return c, st
	case k == "r":
		if i+1 == len(ids) {
			return cmd, parseMore
		}
		cmd.name, cmd.arg = "r", keys[i+1].Text
		if cmd.arg == "" {
			return cmd, parseInvalid
		}
		return done(cmd, i+2)
	case normalCommands[k]:
		cmd.name = k
		return done(cmd, i+1)
	}
	return parseMotion(cmd, ids, keys, i, start)
}

func parseMotion(cmd command, ids []string, keys []Key, i, start int) (command, parseStatus) {
	k := ids[i]
	switch {
	case simpleMotions[k]:
		cmd.name = k
		cmd.keys = keys[start : i+1]
		return cmd, parseDone
	case argMotions[k]:
		if i+1 == len(ids) {
			return cmd, parseMore
		}
		cmd.name, cmd.arg = k, keys[i+1].Text
		if cmd.arg == "" {
			return cmd, parseInvalid
		}
		cmd.keys = keys[start : i+2]
		return cmd, parseDone
	case k == "g":
		if i+1 == len(ids) {
			return cmd, parseMore
		}
		switch ids[i+1] {
		case "g", "e", "E", "_":
			cmd.name = "g" + ids[i+1]
			cmd.keys = keys[start : i+2]
			return cmd, parseDone
		}
	}
	return cmd, parseInvalid
}
