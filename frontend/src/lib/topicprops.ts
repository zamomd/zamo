// splitNames reads a comma-separated list of names as its owner typed it:
// trimmed, without the empty pieces a trailing comma leaves.
export function splitNames(text: string): string[] {
	return text
		.split(",")
		.map((name) => name.trim())
		.filter((name) => name !== "")
}

// unreferenceable reports a name no [[Topic]] reference can carry, which the
// backend refuses for an alias and a tag alike.
export function unreferenceable(name: string): boolean {
	return /[[\]]/.test(name)
}

// tagKey is the key two spellings of one tag share — the same folding the Go
// side's TagKey does, so a filter chosen here matches what storage holds.
export function tagKey(tag: string): string {
	return tag
		.trim()
		.replace(/^#+/, "")
		.split(/\s+/)
		.filter((f) => f !== "")
		.join(" ")
		.toLowerCase()
}

export interface TagCount {
	key: string
	// name is the tag as first written, which is what a chip shows.
	name: string
	topics: number
}

// tagCounts lists every tag the base holds with how many topics carry it,
// ordered by name so the row of chips does not reshuffle as counts change.
export function tagCounts(tags: ReadonlyMap<string, string[]>): TagCount[] {
	const byKey = new Map<string, TagCount>()
	for (const list of tags.values()) {
		for (const tag of list) {
			const key = tagKey(tag)
			const seen = byKey.get(key)
			if (seen === undefined) {
				byKey.set(key, { key, name: tag, topics: 1 })
			} else {
				seen.topics += 1
			}
		}
	}
	return [...byKey.values()].sort((a, b) => a.key.localeCompare(b.key))
}

// hasTag reports whether the topic carries the tag with the given key.
export function hasTag(tags: ReadonlyMap<string, string[]>, topicID: string, key: string): boolean {
	return tags.get(topicID)?.some((tag) => tagKey(tag) === key) ?? false
}
