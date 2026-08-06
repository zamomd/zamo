# Judges the comments a change adds. Reads the added line numbers from ADDED
# (one per line), then the source itself, so a block is measured whole even
# when the change touched one line of it.
#
# A comment says why the code is the way it is. What the code was before, and
# which approach lost, belong to the commit message: a reader a year later has
# no way to tell a stale note from a live one.
#
# Two levels, because the evidence supports only one of them as a hard stop.
# The blocking phrases appear nowhere in this repository's history, so they
# cost nothing to forbid. Contrast phrasing and long blocks are written all
# over the existing code, so they are reported and left to a human.

BEGIN {
	FS = "\n"
	forbidden = "used to be|previously|formerly|was renamed"
	# Often a rejected approach, sometimes a real contract ("must yield
	# ErrLLMUnavailable, not an empty string"). The two cannot be told apart
	# mechanically, so this warns. "no longer" is left out on purpose: every
	# occurrence in this repository describes data at runtime, never an edit.
	suspect = "instead of|rather than|, not "
	inner = 3  # a comment inside the code
	doc = 6    # a doc comment introducing an exported name
}

FILENAME == ADDED { added[$1 + 0] = 1; next }

# A comment line: //, /* ... */, or the continuation of a block.
function commentLine(s) {
	return s ~ /^[[:space:]]*(\/\/|\/\*|\*[^\/])/ || s ~ /^[[:space:]]*\*\/[[:space:]]*$/
}

# An exported declaration, Go or TypeScript: the block above it is a doc
# comment and is allowed to be longer.
function exported(s) {
	return s ~ /^(func|type|const|var|package)[[:space:]]+[A-Z]/ ||
		s ~ /^func[[:space:]]+\([^)]*\)[[:space:]]+[A-Z]/ ||
		s ~ /^export[[:space:]]/
}

function close_block(next_line,   len, limit, i) {
	if (start == 0) {
		return
	}
	for (i = start; i <= end; i++) {
		if (i in added) {
			touched = 1
		}
	}
	if (touched) {
		len = end - start + 1
		limit = exported(next_line) ? doc : inner
		if (len > limit) {
			printf "WARN  %s:%d: comment block of %d lines (limit %d)\n", NAME, start, len, limit
		}
	}
	start = 0
	touched = 0
}

commentLine($0) {
	if (start == 0) {
		start = FNR
	}
	end = FNR
	if (FNR in added) {
		if (tolower($0) ~ forbidden) {
			printf "BLOCK %s:%d:%s\n", NAME, FNR, $0
			failed = 1
		} else if (tolower($0) ~ suspect) {
			printf "WARN  %s:%d:%s\n", NAME, FNR, $0
		}
	}
	next
}

{ close_block($0) }

END {
	close_block("")
	exit failed
}
