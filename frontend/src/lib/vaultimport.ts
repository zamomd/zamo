// The Import card's own logic: what a plan lets the user do and how its
// findings are shown. Pure functions, so the card stays a thin view.
import type {
	Conflict,
	Plan,
	Warning,
} from "../../bindings/github.com/zamomd/zamo/internal/modules/vaultimport/models";

export interface Group<T> {
	kind: string;
	count: number;
	items: T[];
}

/** Groups findings by kind, kinds in first-seen order. */
export function groupByKind<T extends Warning | Conflict>(items: T[] | null): Group<T>[] {
	const groups: Group<T>[] = [];
	for (const item of items ?? []) {
		const g = groups.find((g) => g.kind === item.kind);
		if (g) {
			g.count++;
			g.items.push(item);
		} else {
			groups.push({ kind: item.kind, count: 1, items: [item] });
		}
	}
	return groups;
}

/** A plan can be applied when at least one page survived the conflicts. */
export function canApply(plan: Plan | null): boolean {
	return plan !== null && plan.pages > 0;
}

export function summaryLine(plan: Plan): string {
	return `${plan.format} at ${plan.dir}: ${count(plan.pages, "page")}, ${count(plan.blocks, "block")}, ${count(plan.links, "link")}`;
}

export function count(n: number, noun: string): string {
	return `${n} ${noun}${n === 1 ? "" : "s"}`;
}

const kinds: Record<string, string> = {
	// warnings
	frontmatter: "Frontmatter kept as text",
	alias: "Link aliases dropped",
	anchor: "Heading and block anchors dropped",
	embed: "Embeds turned into plain references",
	path: "Folder paths dropped from links",
	properties: "Properties kept as text",
	"prose-before-bullets": "Text above the first bullet became a block",
	"broken-link": "Links to pages that do not exist",
	"broken-block-ref": "Block references to blocks that do not exist",
	// conflicts
	unnamed: "The name cannot be a topic name",
	exists: "A topic of this name is already in the base",
	duplicate: "Another page of the vault has the same name",
};

/** A reader's label for a warning or conflict kind. */
export function describeKind(kind: string): string {
	return kinds[kind] ?? kind;
}
