import type { Category } from "./api";

/** Categories are capped at depth 3 (docs/04-DATA-MODEL.md § 2); adding a
 * child under a depth-3 category would exceed that. */
export const MAX_CATEGORY_DEPTH = 3;

export interface CategoryTreeNode {
  key: string;
  title: string;
  category: Category;
  children: CategoryTreeNode[];
}

/** Builds a tree from `GET /categories`'s flat, `parentId`-linked list (the
 * response is not cursor-paginated — the client builds the hierarchy,
 * `docs/05-API.md` § Catalogue). Each level is sorted by `sortOrder`. */
export function buildCategoryTree(categories: Category[]): CategoryTreeNode[] {
  const byParent = new Map<string | null, Category[]>();
  for (const category of categories) {
    const siblings = byParent.get(category.parentId) ?? [];
    siblings.push(category);
    byParent.set(category.parentId, siblings);
  }

  function build(parentId: string | null): CategoryTreeNode[] {
    const children = [...(byParent.get(parentId) ?? [])].sort((a, b) => a.sortOrder - b.sortOrder);
    return children.map((category) => ({
      key: category.id,
      title: category.name,
      category,
      children: build(category.id),
    }));
  }

  return build(null);
}

/** A category's depth (root = 1), by walking its `parentId` chain in the
 * flat list. `null` (no category / top level) is depth 0. */
export function categoryDepth(categoryId: string | null, categories: Category[]): number {
  const byId = new Map(categories.map((category) => [category.id, category]));
  let depth = 0;
  let currentId = categoryId;
  while (currentId != null) {
    depth += 1;
    currentId = byId.get(currentId)?.parentId ?? null;
  }
  return depth;
}

export interface CategorySelectNode {
  value: string;
  title: string;
  children: CategorySelectNode[];
}

/** `buildCategoryTree`'s nodes, reshaped for AntD `TreeSelect`'s `treeData`
 * (`value`/`title`/`children`) — used by both the product list's category
 * filter and the product form's category field. */
export function buildCategoryTreeSelectData(categories: Category[]): CategorySelectNode[] {
  function toNode(node: CategoryTreeNode): CategorySelectNode {
    return { value: node.key, title: node.title, children: node.children.map(toNode) };
  }
  return buildCategoryTree(categories).map(toNode);
}
