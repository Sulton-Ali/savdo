import type { Locale } from "@savdo/i18n";
import { Alert, Form, type FormInstance, Input, Space, Switch, TimePicker } from "antd";
import type { Dayjs } from "dayjs";
import dayjs from "dayjs";
import { useTranslation } from "react-i18next";

import type { ContentHours, ContentHoursDay, ContentResource } from "../../../lib/content";
import { ContentBlockCard } from "./ContentBlockCard";

/** Fixed weekday order (D-107) — the form always renders exactly these 7
 * rows, no add/remove. */
const WEEKDAYS: ContentHoursDay["day"][] = ["mon", "tue", "wed", "thu", "fri", "sat", "sun"];

interface HoursDayFormValue {
  closed: boolean;
  open?: Dayjs;
  close?: Dayjs;
}

interface HoursFormValues {
  days: HoursDayFormValue[];
  note?: string;
}

/** Day rows are locale-independent (D-107): when this locale has no saved
 * `hours` row yet, pre-fill `days` from `uz`'s row so the user only has to
 * add a translated `note`, not redo all 7 rows. Once this locale has its own
 * saved row, its own `days` are used (never silently overwritten by uz). */
function hoursInitialValues(
  resource: ContentResource | undefined,
  locale: Locale,
): Partial<HoursFormValues> {
  const ownData = resource?.locales[locale]?.data as ContentHours | undefined;
  const fallbackData = ownData ?? (resource?.locales.uz?.data as ContentHours | undefined);
  const days = WEEKDAYS.map((day) => {
    const row = fallbackData?.days.find((d) => d.day === day);
    return {
      closed: row?.closed ?? false,
      open: row?.open ? dayjs(row.open, "HH:mm") : undefined,
      close: row?.close ? dayjs(row.close, "HH:mm") : undefined,
    };
  });
  return { days, note: ownData?.note };
}

function buildHoursPayload(values: HoursFormValues): ContentHours {
  const days: ContentHoursDay[] = WEEKDAYS.map((day, index) => {
    const row = values.days?.[index];
    const closed = row?.closed ?? false;
    const entry: ContentHoursDay = { day, closed };
    if (!closed) {
      if (row?.open) {
        entry.open = row.open.format("HH:mm");
      }
      if (row?.close) {
        entry.close = row.close.format("HH:mm");
      }
    }
    return entry;
  });
  const payload: ContentHours = { days };
  const note = values.note?.trim();
  if (note) {
    payload.note = note;
  }
  return payload;
}

function DayRow({
  form,
  index,
  day,
}: {
  form: FormInstance<HoursFormValues>;
  index: number;
  day: ContentHoursDay["day"];
}) {
  const { t } = useTranslation();
  const dayLabel = t(`content.hours.days.${day}`);
  const closed = Form.useWatch(["days", index, "closed"], form);

  return (
    <Space align="baseline" style={{ display: "flex", marginBottom: 8 }}>
      <span style={{ display: "inline-block", width: 110 }}>{dayLabel}</span>
      <Form.Item name={["days", index, "closed"]} valuePropName="checked" noStyle>
        <Switch
          aria-label={`${dayLabel} ${t("content.hours.closedToggle")}`}
          checkedChildren={t("content.hours.closedToggle")}
          unCheckedChildren={t("content.hours.open")}
        />
      </Form.Item>
      <Form.Item name={["days", index, "open"]} noStyle rules={closed ? [] : [{ required: true }]}>
        <TimePicker
          format="HH:mm"
          disabled={closed}
          aria-label={`${dayLabel} ${t("content.hours.open")}`}
        />
      </Form.Item>
      <Form.Item name={["days", index, "close"]} noStyle rules={closed ? [] : [{ required: true }]}>
        <TimePicker
          format="HH:mm"
          disabled={closed}
          aria-label={`${dayLabel} ${t("content.hours.close")}`}
        />
      </Form.Item>
    </Space>
  );
}

export function HoursCard({
  locale,
  resource,
  isPending,
}: {
  locale: Locale;
  resource: ContentResource | undefined;
  isPending: boolean;
}) {
  const { t } = useTranslation();
  return (
    <ContentBlockCard<HoursFormValues, ContentHours>
      contentKey="hours"
      title={t("content.keys.hours")}
      locale={locale}
      resource={resource}
      isPending={isPending}
      buildInitialValues={hoursInitialValues}
      buildPayload={buildHoursPayload}
      hint={
        locale !== "uz" ? (
          <Alert
            type="info"
            showIcon
            message={t("content.hours.fallbackHint")}
            style={{ marginBottom: 16 }}
          />
        ) : undefined
      }
    >
      {(form) => (
        <>
          {WEEKDAYS.map((day, index) => (
            <DayRow key={day} form={form} index={index} day={day} />
          ))}
          <Form.Item name="note" label={t("content.hours.note")}>
            <Input />
          </Form.Item>
        </>
      )}
    </ContentBlockCard>
  );
}
