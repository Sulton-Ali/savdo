import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { App as AntApp, ConfigProvider } from "antd";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../lib/api", () => ({
  api: { GET: vi.fn(), POST: vi.fn(), PATCH: vi.fn(), DELETE: vi.fn() },
}));

// The crop step draws onto a `<canvas>`, which jsdom does not implement —
// stub `ImageCropModal` with a trivial component exposing "apply"/"cancel"
// buttons that call the same props the real modal calls, so the gallery's
// upload pipeline (POST /media → POST .../images) is exercised without any
// real cropping.
vi.mock("../ImageCropModal", () => ({
  ImageCropModal: ({
    onCropped,
    onCancel,
  }: {
    onCropped: (blob: Blob) => void;
    onCancel: () => void;
  }) => (
    <div>
      <button type="button" onClick={() => onCropped(new Blob(["x"], { type: "image/jpeg" }))}>
        apply-crop
      </button>
      <button type="button" onClick={onCancel}>
        cancel-crop
      </button>
    </div>
  ),
}));

import type { ProductImage, Variant } from "../../../catalog/api";
import { i18next } from "../../../i18n";
import { api } from "../../../lib/api";
import { ImageGallery } from "../ImageGallery";

const mockedApi = vi.mocked(api, { deep: true });

function image(id: string, sortOrder: number, overrides: Partial<ProductImage> = {}): ProductImage {
  return {
    id,
    mediaId: `media-${id}`,
    variantId: null,
    isCover: sortOrder === 0,
    sortOrder,
    urls: { thumb: `${id}-thumb`, card: `${id}-card`, full: `${id}-full` },
    ...overrides,
  };
}

function variant(overrides: Partial<Variant> = {}): Variant {
  return {
    id: "v1",
    sku: null,
    barcode: null,
    attributes: { color: "blue" },
    priceOverride: null,
    isActive: true,
    ...overrides,
  };
}

function apiResult(data: unknown, status = 200) {
  return { data, error: undefined, response: new Response(null, { status }) } as never;
}

function apiError(code: string, details: Record<string, unknown> = {}, status = 400) {
  return {
    data: undefined,
    error: { error: { code, details } },
    response: new Response(null, { status }),
  } as never;
}

/** A promise whose resolution the test controls from the outside. */
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });
  return { promise, resolve };
}

/** Whether an antd `Select`'s search input is currently disabled — checked
 * via the wrapper's class rather than the input's own `disabled` attribute,
 * which rc-select does not always set on the input itself. */
function isSelectDisabled(input: HTMLElement): boolean {
  return input.closest(".ant-select")?.classList.contains("ant-select-disabled") ?? false;
}

/**
 * Clicks the most recently opened dropdown's option. A closed `Select`'s
 * dropdown portal can linger in `document.body` (rc-select hides rather
 * than unmounts it), so with more than one `Select` on the page the
 * *last* `.ant-select-item-option` node — the most recently appended
 * portal — is the one actually open, not necessarily the first.
 */
async function clickOpenDropdownOption(): Promise<void> {
  const option = await waitFor(() => {
    const options = document.querySelectorAll(".ant-select-item-option");
    const last = options[options.length - 1];
    if (!last) {
      throw new Error("dropdown option not rendered yet");
    }
    return last;
  });
  fireEvent.click(option);
}

function renderGallery(
  images: ProductImage[],
  options: { variants?: Variant[]; queryClient?: QueryClient } = {},
) {
  const queryClient =
    options.queryClient ?? new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <ConfigProvider theme={{ token: { motion: false } }}>
      <QueryClientProvider client={queryClient}>
        <AntApp>
          <ImageGallery
            productId="p1"
            images={images}
            variants={options.variants ?? []}
            attributeDefinitions={[]}
            canWrite
          />
        </AntApp>
      </QueryClientProvider>
    </ConfigProvider>,
  );
}

describe("ImageGallery", () => {
  const originalCreateObjectURL = URL.createObjectURL;
  const originalRevokeObjectURL = URL.revokeObjectURL;

  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  beforeEach(() => {
    mockedApi.POST.mockReset();
    mockedApi.PATCH.mockReset();
    mockedApi.DELETE.mockReset();
    URL.createObjectURL = vi.fn(() => "blob:mock");
    URL.revokeObjectURL = vi.fn();
  });

  afterEach(() => {
    cleanup();
    URL.createObjectURL = originalCreateObjectURL;
    URL.revokeObjectURL = originalRevokeObjectURL;
  });

  it("disables the upload button once the product has 8 images", () => {
    const images = Array.from({ length: 8 }, (_, i) => image(`img${i}`, i));
    renderGallery(images);

    const uploadButton = screen.getByRole("button", { name: "Upload image" }) as HTMLButtonElement;
    expect(uploadButton.disabled).toBe(true);
    expect(screen.getByText("This product already has the maximum of 8 images.")).toBeTruthy();
  });

  it("posts the full ordered id list when an image is moved down", async () => {
    mockedApi.PATCH.mockResolvedValueOnce(apiResult({ items: [] }));
    renderGallery([image("a", 0), image("b", 1), image("c", 2)]);

    const moveDownButtons = screen.getAllByRole("button", { name: "Move down" });
    fireEvent.click(moveDownButtons[0] as HTMLElement);

    await waitFor(() => {
      expect(mockedApi.PATCH).toHaveBeenCalledWith("/products/{id}/images/order", {
        params: { path: { id: "p1" } },
        body: { imageIds: ["b", "a", "c"] },
      });
    });
  });

  it("PATCHes isCover:true on the image itself when a non-cover image is set as cover", async () => {
    mockedApi.PATCH.mockResolvedValueOnce(apiResult(image("b", 1, { isCover: true })));
    renderGallery([image("a", 0), image("b", 1)]);

    fireEvent.click(screen.getByRole("button", { name: "Set as cover" }));

    await waitFor(() => {
      expect(mockedApi.PATCH).toHaveBeenCalledWith("/products/{id}/images/{imageId}", {
        params: { path: { id: "p1", imageId: "b" } },
        body: { isCover: true },
      });
    });
  });

  it("uploads via POST /media with FormData, then attaches with the returned id", async () => {
    mockedApi.POST.mockImplementation(((path: string) => {
      if (path === "/media") {
        return Promise.resolve(
          apiResult(
            {
              id: "media-new",
              mime: "image/jpeg",
              width: 800,
              height: 600,
              sizeBytes: 1234,
              urls: { thumb: "t", card: "c", full: "f" },
            },
            201,
          ),
        );
      }
      if (path === "/products/{id}/images") {
        return Promise.resolve(apiResult(image("new-image", 0, { mediaId: "media-new" }), 201));
      }
      throw new Error(`unexpected POST ${path}`);
    }) as never);

    const { container } = renderGallery([]);

    const fileInput = container.querySelector('input[type="file"]') as HTMLInputElement;
    const file = new File(["content"], "photo.jpg", { type: "image/jpeg" });
    fireEvent.change(fileInput, { target: { files: [file] } });

    fireEvent.click(await screen.findByText("apply-crop"));

    await waitFor(() => {
      expect(mockedApi.POST).toHaveBeenCalledTimes(2);
    });

    // `api.POST` is mocked, so openapi-fetch's own machinery never actually
    // calls the `bodySerializer` `uploadMedia` (`catalog/api.ts`) hands it —
    // invoke it here the same way the real client would, to prove it turns
    // the file into real `multipart/form-data` `FormData` rather than a
    // plain JSON body.
    const calls = mockedApi.POST.mock.calls as unknown as Array<
      [string, { body: { file: Blob }; bodySerializer: (body: unknown) => FormData }]
    >;
    const mediaCall = calls.find(([path]) => path === "/media");
    expect(mediaCall).toBeDefined();
    const [, mediaOptions] = mediaCall as (typeof calls)[number];
    expect(mediaOptions.body.file).toBeInstanceOf(Blob);
    const serialized = mediaOptions.bodySerializer(mediaOptions.body);
    expect(serialized).toBeInstanceOf(FormData);
    // FormData wraps a filename-less `Blob` into a `File` on append (WHATWG
    // spec), so compare content/type rather than object identity.
    const appendedFile = serialized.get("file") as Blob;
    expect(appendedFile.type).toBe(mediaOptions.body.file.type);
    expect(appendedFile.size).toBe(mediaOptions.body.file.size);

    expect(mockedApi.POST).toHaveBeenCalledWith("/products/{id}/images", {
      params: { path: { id: "p1" } },
      body: { mediaId: "media-new", isCover: true },
    });
  });

  // T5 (phase-3): retagging PATCHes the image directly
  // (`PATCH /products/{id}/images/{imageId}`, D-43) instead of the old
  // detach/reattach/reorder workaround.
  it("PATCHes the image directly when retagging, not remove/add/reorder", async () => {
    mockedApi.PATCH.mockResolvedValueOnce(apiResult(image("a", 0, { variantId: "v1" })));

    renderGallery([image("a", 0)], { variants: [variant({ id: "v1" })] });

    fireEvent.mouseDown(screen.getByLabelText("Variant"));
    await clickOpenDropdownOption();

    await waitFor(() => {
      expect(mockedApi.PATCH).toHaveBeenCalledWith("/products/{id}/images/{imageId}", {
        params: { path: { id: "p1", imageId: "a" } },
        body: { variantId: "v1" },
      });
    });
    expect(mockedApi.DELETE).not.toHaveBeenCalled();
    expect(mockedApi.POST).not.toHaveBeenCalled();
  });

  it("PATCHes variantId: null when the variant tag is cleared (untag)", async () => {
    mockedApi.PATCH.mockResolvedValueOnce(apiResult(image("a", 0, { variantId: null })));

    const { container } = renderGallery([image("a", 0, { variantId: "v1" })], {
      variants: [variant({ id: "v1" })],
    });

    const clearIcon = container.querySelector(".ant-select-clear") as HTMLElement;
    expect(clearIcon).toBeTruthy();
    fireEvent.mouseDown(clearIcon);
    fireEvent.click(clearIcon);

    await waitFor(() => {
      expect(mockedApi.PATCH).toHaveBeenCalledWith("/products/{id}/images/{imageId}", {
        params: { path: { id: "p1", imageId: "a" } },
        body: { variantId: null },
      });
    });
  });

  it("notifies on a failed retag", async () => {
    mockedApi.PATCH.mockResolvedValueOnce(
      apiError("VALIDATION_FAILED", { fields: { variantId: "invalid" } }, 400),
    );

    renderGallery([image("a", 0)], { variants: [variant({ id: "v1" })] });

    fireEvent.mouseDown(screen.getByLabelText("Variant"));
    await clickOpenDropdownOption();

    expect(await screen.findByText("Something went wrong. Please try again.")).toBeTruthy();
  });

  // Review follow-up (phase-2/t6b, still applies to the single-PATCH
  // flow): locking must key off a Set of in-flight image ids, not the
  // single mutation's `variables` (which only ever reflects the most
  // recent `.mutate()` call) — otherwise starting a second update while a
  // first is still pending would silently unlock the first.
  it("keeps each image's controls locked independently when two retags overlap", async () => {
    const patches = [deferred<unknown>(), deferred<unknown>()];
    let patchCall = 0;
    mockedApi.PATCH.mockImplementation((() => {
      const call = patches[patchCall];
      patchCall += 1;
      return call?.promise;
    }) as never);

    renderGallery([image("a", 0), image("b", 1)], { variants: [variant({ id: "v1" })] });

    const selects = screen.getAllByLabelText("Variant") as HTMLInputElement[];
    expect(selects).toHaveLength(2);

    fireEvent.mouseDown(selects[0] as HTMLInputElement);
    await clickOpenDropdownOption();
    fireEvent.mouseDown(selects[1] as HTMLInputElement);
    await clickOpenDropdownOption();

    // Both retags are now in flight (both PATCH calls pending) — both
    // Selects must stay locked, not just the most recently started one.
    await waitFor(() => {
      expect(isSelectDisabled(selects[0] as HTMLInputElement)).toBe(true);
      expect(isSelectDisabled(selects[1] as HTMLInputElement)).toBe(true);
    });

    patches[0]?.resolve(apiResult(image("a", 0, { variantId: "v1" })));
    // "b"'s PATCH hasn't resolved yet — it must remain locked throughout.
    expect(isSelectDisabled(selects[1] as HTMLInputElement)).toBe(true);

    patches[1]?.resolve(apiResult(image("b", 1, { variantId: "v1" })));

    await waitFor(() => {
      expect(isSelectDisabled(selects[0] as HTMLInputElement)).toBe(false);
      expect(isSelectDisabled(selects[1] as HTMLInputElement)).toBe(false);
    });
  });
});
