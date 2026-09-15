package chaptarr

import "strings"

// normalizePath lowercases a filesystem path and converts backslashes to
// forward slashes, so paths reported by Chaptarr (which may run on a
// different OS/mount than this app) and this app's own library-relative
// paths can be compared on equal footing.
func normalizePath(p string) string {
	p = strings.ToLower(strings.ReplaceAll(p, `\`, "/"))
	return strings.Trim(p, "/")
}

// MatchByPath finds the Chaptarr book, if any, whose reported path most
// plausibly corresponds to localRelPath (this app's own library-relative
// file path, e.g. "Brandon Sanderson/Mistborn/Mistborn.epub").
//
// Exact path equality isn't used because Chaptarr's own library root
// almost never matches this app's LIBRARY_PATH — the two tools usually see
// the same files through different mount points or directory prefixes.
// Instead this matches on path *suffix* containment: the local path must
// end with the same trailing segments as one of Chaptarr's reported paths
// (or vice versa), which tolerates a differing root while still being
// specific enough that "the trailing filename+folder match" is a
// meaningful signal. The longest suffix match wins when multiple candidates
// share some overlap, so a book matching only by filename (a weak signal)
// loses to one that also matches the enclosing author/series folder.
func MatchByPath(books []Book, localRelPath string) (Book, bool) {
	local := normalizePath(localRelPath)
	if local == "" {
		return Book{}, false
	}

	var best Book
	bestScore := -1
	found := false
	for _, b := range books {
		for _, p := range b.Paths {
			score := suffixOverlapScore(local, normalizePath(p))
			if score > bestScore {
				bestScore = score
				best = b
				found = true
			}
		}
	}
	// Require at least the filename itself to match — a score of 0 (no
	// shared trailing segment at all) is not a match.
	if bestScore <= 0 {
		return Book{}, false
	}
	return best, found
}

// suffixOverlapScore counts how many trailing "/"-separated segments a and
// b share, comparing from the end of each path. Returns 0 if even the
// final segment (the filename) differs.
func suffixOverlapScore(a, b string) int {
	as := strings.Split(a, "/")
	bs := strings.Split(b, "/")
	score := 0
	for i, j := len(as)-1, len(bs)-1; i >= 0 && j >= 0; i, j = i-1, j-1 {
		if as[i] != bs[j] {
			break
		}
		score++
	}
	return score
}
