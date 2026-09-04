import { useMutation, useQueryClient } from "@tanstack/react-query";
import { App, Button, Card, Select, Space, Tag } from "antd";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import {
  type AttributeDefinition,
  type AttributeValues,
  createVariant,
  type Variant,
} from "../../catalog/api";
import {
  canonicalAttributesKey,
  cartesianAttributeCombinations,
  combinationsToCreate,
} from "../../catalog/variants";

/** Attribute values, ≤ 64 chars each (`contracts/openapi.yaml`
 * `AttributeValues`/`VariantCreate` description). */
const MAX_VALUE_LENGTH = 64;

/** Thrown by the generator's `mutationFn` when `createVariant` fails
 * partway through the list, so `onError` can report exactly how many
 * combinations made it in before the one that failed. */
class PartialGenerateFailure extends Error {
  constructor(
    readonly created: number,
    readonly total: number,
  ) {
    super(`created ${created} of ${total} variants before an error`);
  }
}

/**
 * The size × colour matrix generator (T6b spec): pick which attributes to
 * vary, type each attribute's values as tags, preview the Cartesian
 * product, then create one variant per combination that doesn't already
 * exist. Variants are posted **sequentially** and stop at the first error —
 * a bulk "create N" call doesn't exist in the contract (only
 * `POST /products/{id}/variants` for one variant at a time), and stopping
 * lets the user see exactly which combination failed and retry from there.
 */
export function VariantMatrixGenerator({
  productId,
  attributeDefinitions,
  existingVariants,
}: {
  productId: string;
  attributeDefinitions: AttributeDefinition[];
  existingVariants: Variant[];
}) {
  const { t } = useTranslation();
  const { notification } = App.useApp();
  const queryClient = useQueryClient();

  const [selectedCodes, setSelectedCodes] = useState<string[]>([]);
  const [valuesByCode, setValuesByCode] = useState<Record<string, string[]>>({});
  // Set (and left set) per attribute code when `setValues` drops a blank or
  // over-length entry, so the user sees why their tag didn't stick.
  const [droppedValueCodes, setDroppedValueCodes] = useState<Record<string, boolean>>({});

  const combinations = useMemo(() => cartesianAttributeCombinations(valuesByCode), [valuesByCode]);
  const toCreate = useMemo(
    () => combinationsToCreate(combinations, existingVariants),
    [combinations, existingVariants],
  );
  const toCreateKeys = useMemo(() => new Set(toCreate.map(canonicalAttributesKey)), [toCreate]);

  const generateMutation = useMutation({
    mutationFn: async (attributesList: AttributeValues[]) => {
      let created = 0;
      for (const attributes of attributesList) {
        try {
          await createVariant(productId, { attributes });
        } catch {
          throw new PartialGenerateFailure(created, attributesList.length);
        }
        created += 1;
      }
      return created;
    },
    onSuccess: async (created) => {
      await queryClient.invalidateQueries({ queryKey: ["variants", productId] });
      notification.success({ message: t("catalog.variants.matrix.created", { count: created }) });
      setSelectedCodes([]);
      setValuesByCode({});
    },
    onError: (error) => {
      queryClient.invalidateQueries({ queryKey: ["variants", productId] });
      if (error instanceof PartialGenerateFailure) {
        notification.error({
          message: t("catalog.variants.matrix.partial", {
            created: error.created,
            total: error.total,
          }),
        });
        return;
      }
      notification.error({ message: t("errors.generic") });
    },
  });

  function toggleAttribute(code: string, checked: boolean) {
    setSelectedCodes((current) =>
      checked ? [...current, code] : current.filter((c) => c !== code),
    );
    if (!checked) {
      setValuesByCode((current) => {
        const next = { ...current };
        delete next[code];
        return next;
      });
      setDroppedValueCodes((current) => {
        const next = { ...current };
        delete next[code];
        return next;
      });
    }
  }

  function setValues(code: string, values: string[]) {
    const valid = values.filter((v) => v.trim().length > 0 && v.length <= MAX_VALUE_LENGTH);
    setValuesByCode((current) => ({ ...current, [code]: valid }));
    setDroppedValueCodes((current) => ({ ...current, [code]: valid.length !== values.length }));
  }

  return (
    <Card title={t("catalog.variants.matrix.title")} size="small" style={{ marginBottom: 16 }}>
      <Space direction="vertical" style={{ width: "100%" }} size="middle">
        {attributeDefinitions.map((def) => {
          const checked = selectedCodes.includes(def.code);
          return (
            <div key={def.id} data-testid={`matrix-attribute-${def.code}`}>
              <Button
                size="small"
                type={checked ? "primary" : "default"}
                onClick={() => toggleAttribute(def.code, !checked)}
              >
                {def.name}
              </Button>
              {checked && (
                <>
                  <Select
                    mode="tags"
                    style={{ width: "100%", marginTop: 8 }}
                    placeholder={t("catalog.variants.matrix.valuesPlaceholder")}
                    value={valuesByCode[def.code] ?? []}
                    onChange={(values) => setValues(def.code, values)}
                    open={false}
                    suffixIcon={null}
                    aria-label={def.name}
                  />
                  {droppedValueCodes[def.code] && (
                    <div style={{ color: "#cf1322", fontSize: 12, marginTop: 4 }}>
                      {t("catalog.variants.matrix.invalidValue")}
                    </div>
                  )}
                </>
              )}
            </div>
          );
        })}

        {combinations.length > 0 && (
          <div>
            <div style={{ marginBottom: 8 }}>{t("catalog.variants.matrix.preview")}</div>
            <Space wrap>
              {combinations.map((combo) => {
                const key = canonicalAttributesKey(combo);
                return (
                  <Tag key={key} color={toCreateKeys.has(key) ? "blue" : "default"}>
                    {Object.values(combo).join(" / ")}
                  </Tag>
                );
              })}
            </Space>
          </div>
        )}

        <Button
          type="primary"
          disabled={toCreate.length === 0}
          loading={generateMutation.isPending}
          onClick={() => generateMutation.mutate(toCreate)}
        >
          {toCreate.length === 0 && combinations.length > 0
            ? t("catalog.variants.matrix.noNew")
            : t("catalog.variants.matrix.generate", { count: toCreate.length })}
        </Button>
      </Space>
    </Card>
  );
}
