import { Pressable, ScrollView } from "react-native";

import { Text } from "@/components/ui/text";
import { cn } from "@/lib/cn";

export interface ChipOption<T extends string> {
  value: T;
  label: string;
}

/**
 * A horizontally-scrolling row of selectable chips — this app has no
 * `Select`/dropdown primitive (`components/ui` only has `button`, `card`,
 * `input`, `text`; D-78's dependency list has no picker library either), and
 * a shop's locations/suppliers/adjustment reasons are all short, fixed-ish
 * lists, same shape as `SettingsSheet.tsx`'s language switch this mirrors.
 * Shared by the Stock tab's location picker (`app/(app)/stock/index.tsx`),
 * the adjustment form's location/reason fields
 * (`app/(app)/stock/adjust.tsx`) and the new-purchase form's supplier/
 * location fields (`app/(app)/stock/purchases/new.tsx`) — kept in
 * `features/stock` (imported by `features/purchases` too) rather than
 * duplicated three times, since this app has no shared UI location either
 * feature's task scope covers.
 */
export function ChipGroup<T extends string>({
  options,
  value,
  onChange,
  disabled,
  accessibilityLabel,
}: {
  options: ChipOption<T>[];
  value: T | null;
  onChange: (value: T) => void;
  disabled?: boolean;
  accessibilityLabel?: string;
}) {
  return (
    <ScrollView
      horizontal
      showsHorizontalScrollIndicator={false}
      accessibilityLabel={accessibilityLabel}
      contentContainerStyle={{ gap: 8, paddingVertical: 2 }}
    >
      {options.map((option) => {
        const selected = value === option.value;
        return (
          <Pressable
            key={option.value}
            accessibilityRole="button"
            accessibilityState={{ selected, disabled }}
            disabled={disabled}
            onPress={() => onChange(option.value)}
            className={cn(
              "h-10 items-center justify-center rounded-md border px-3",
              selected ? "border-primary bg-primary" : "border-input bg-background",
              disabled && "opacity-50",
            )}
          >
            <Text className={selected ? "text-primary-foreground" : undefined}>
              {option.label}
            </Text>
          </Pressable>
        );
      })}
    </ScrollView>
  );
}
