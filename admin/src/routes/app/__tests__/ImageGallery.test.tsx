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

import type { ProductImage } from "../../../catalog/api";
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

function apiResult(data: unknown, status = 200) {
  return { data, error: undefined, response: new Response(null, { status }) } as never;
}

function renderGallery(images: ProductImage[]) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <ConfigProvider theme={{ token: { motion: false } }}>
      <QueryClientProvider client={queryClient}>
        <AntApp>
          <ImageGallery
            productId="p1"
            images={images}
            variants={[]}
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

  it("posts coverImageId when a non-cover image is set as cover", async () => {
    mockedApi.PATCH.mockResolvedValueOnce(apiResult({ items: [] }));
    renderGallery([image("a", 0), image("b", 1)]);

    fireEvent.click(screen.getByRole("button", { name: "Set as cover" }));

    await waitFor(() => {
      expect(mockedApi.PATCH).toHaveBeenCalledWith("/products/{id}/images/order", {
        params: { path: { id: "p1" } },
        body: { imageIds: ["a", "b"], coverImageId: "b" },
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
});
