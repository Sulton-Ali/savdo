import { Modal, Segmented, Slider } from "antd";
import { useState } from "react";
import Cropper, { type Area, type MediaSize, type Point } from "react-easy-crop";
import { useTranslation } from "react-i18next";

import { cropImageToBlob } from "../../catalog/crop";

type AspectPreset = "free" | "square" | "portrait";

const ASPECT_RATIOS: Record<Exclude<AspectPreset, "free">, number> = {
  square: 1,
  portrait: 4 / 5,
};

/**
 * The crop step between picking a file and uploading it (T6b spec):
 * `react-easy-crop`'s `Cropper` only reports the crop as percentages/pixels
 * of the source image — it exposes no canvas export itself — so `Apply`
 * hands the last `onCropComplete` pixel rect to `cropImageToBlob`, which
 * draws it onto an off-screen canvas (jsdom has none, so every test mocks
 * either this component or `cropImageToBlob` directly rather than
 * exercising real canvas work).
 *
 * "Free aspect" (the default) uses the source image's own natural aspect
 * ratio — `Cropper` always crops a fixed-ratio rectangle, it does not
 * support a user-resizable crop box, so "free" here means "don't force a
 * different shape onto the photo", not "resize the box by hand". The 1:1
 * and 4:5 presets constrain it further for product/portrait shots.
 */
export function ImageCropModal({
  imageSrc,
  onCancel,
  onCropped,
}: {
  imageSrc: string;
  onCancel: () => void;
  onCropped: (blob: Blob) => void;
}) {
  const { t } = useTranslation();
  const [crop, setCrop] = useState<Point>({ x: 0, y: 0 });
  const [zoom, setZoom] = useState(1);
  const [preset, setPreset] = useState<AspectPreset>("free");
  const [naturalAspect, setNaturalAspect] = useState(4 / 3);
  const [croppedAreaPixels, setCroppedAreaPixels] = useState<Area | null>(null);
  const [applying, setApplying] = useState(false);

  const aspect = preset === "free" ? naturalAspect : ASPECT_RATIOS[preset];

  async function handleApply() {
    if (!croppedAreaPixels) {
      return;
    }
    setApplying(true);
    try {
      const blob = await cropImageToBlob(imageSrc, croppedAreaPixels);
      onCropped(blob);
    } finally {
      setApplying(false);
    }
  }

  function handleMediaLoaded(mediaSize: MediaSize) {
    if (mediaSize.naturalHeight > 0) {
      setNaturalAspect(mediaSize.naturalWidth / mediaSize.naturalHeight);
    }
  }

  return (
    <Modal
      title={t("catalog.images.crop.title")}
      open
      onCancel={onCancel}
      onOk={handleApply}
      okText={t("catalog.images.crop.apply")}
      cancelText={t("catalog.images.crop.cancel")}
      confirmLoading={applying}
      width={520}
      destroyOnHidden
    >
      <Segmented
        value={preset}
        onChange={(value) => setPreset(value as AspectPreset)}
        options={[
          { label: t("catalog.images.crop.free"), value: "free" },
          { label: t("catalog.images.crop.square"), value: "square" },
          { label: t("catalog.images.crop.portrait"), value: "portrait" },
        ]}
        style={{ marginBottom: 12 }}
      />
      <div style={{ position: "relative", height: 320, background: "#000" }}>
        <Cropper
          image={imageSrc}
          crop={crop}
          zoom={zoom}
          aspect={aspect}
          onCropChange={setCrop}
          onZoomChange={setZoom}
          onCropComplete={(_area, areaPixels) => setCroppedAreaPixels(areaPixels)}
          onMediaLoaded={handleMediaLoaded}
        />
      </div>
      <Slider
        min={1}
        max={3}
        step={0.1}
        value={zoom}
        onChange={(value) => setZoom(value as number)}
        style={{ marginTop: 12 }}
      />
    </Modal>
  );
}
