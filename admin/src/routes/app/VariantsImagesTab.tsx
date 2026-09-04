import { useQuery } from "@tanstack/react-query";
import { Skeleton, Space } from "antd";

import { useAuth } from "../../auth/AuthContext";
import { fetchAttributeDefinitions, fetchVariants, type Product } from "../../catalog/api";
import { ImageGallery } from "./ImageGallery";
import { VariantMatrixGenerator } from "./VariantMatrixGenerator";
import { VariantsTable } from "./VariantsTable";

/**
 * The product form's "Variants & images" tab body (T6b spec, replacing the
 * T6a placeholder) — only ever mounted once the product exists (edit mode),
 * since variants and images both hang off a product id.
 */
export function VariantsImagesTab({ product }: { product: Product }) {
  const { can } = useAuth();
  const canWrite = can("catalog.write");

  const { data: attributeDefinitions, isPending: attributesPending } = useQuery({
    queryKey: ["attribute-definitions"],
    queryFn: fetchAttributeDefinitions,
  });
  const { data: variants, isPending: variantsPending } = useQuery({
    queryKey: ["variants", product.id],
    queryFn: () => fetchVariants(product.id),
  });

  if (attributesPending || variantsPending) {
    return <Skeleton active />;
  }

  const defs = attributeDefinitions ?? [];
  const variantList = variants ?? [];

  return (
    <Space direction="vertical" style={{ width: "100%" }} size="large">
      {canWrite && (
        <VariantMatrixGenerator
          productId={product.id}
          attributeDefinitions={defs}
          existingVariants={variantList}
        />
      )}
      <VariantsTable
        productId={product.id}
        variants={variantList}
        attributeDefinitions={defs}
        canWrite={canWrite}
      />
      <ImageGallery
        productId={product.id}
        images={product.images ?? []}
        variants={variantList}
        attributeDefinitions={defs}
        canWrite={canWrite}
      />
    </Space>
  );
}
