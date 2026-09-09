import { type Locale, locales } from "@savdo/i18n";
import { useTranslation } from "react-i18next";
import { Modal, Pressable, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";

import { i18next } from "@/i18n";
import { useLogout } from "@/lib/session";

import { TelegramLinkSection } from "./TelegramLinkSection";
import { Button } from "./ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "./ui/card";
import { Text } from "./ui/text";

/**
 * The header's small settings sheet (deliverable 6): language switch,
 * Telegram account link (Phase 7 T7 deliverable D) and logout. Built from
 * the copied `react-native-reusables` primitives and React Native's own
 * `Modal` — a dedicated `Sheet`/`Dialog` primitive isn't used since this is
 * the only place the app needs one (keep it simple).
 */
export function SettingsSheet({ visible, onClose }: { visible: boolean; onClose: () => void }) {
  const { t } = useTranslation();
  const logout = useLogout();
  const insets = useSafeAreaInsets();

  function handleSelectLocale(locale: Locale) {
    void i18next.changeLanguage(locale);
  }

  function handleLogout() {
    logout.mutate(undefined, { onSettled: onClose });
  }

  return (
    <Modal visible={visible} animationType="fade" transparent onRequestClose={onClose}>
      <Pressable className="flex-1 justify-end bg-black/40" onPress={onClose}>
        <Pressable onPress={(event) => event.stopPropagation()}>
          <Card className="rounded-b-none" style={{ paddingBottom: insets.bottom + 24 }}>
            <CardHeader>
              <CardTitle>{t("settings.title")}</CardTitle>
            </CardHeader>
            <CardContent className="gap-4">
              <View className="gap-2">
                <Text variant="small">{t("lang.switch")}</Text>
                <View className="flex-row gap-2">
                  {locales.map((locale) => (
                    <Button
                      key={locale}
                      variant={i18next.language === locale ? "default" : "outline"}
                      className="flex-1"
                      onPress={() => handleSelectLocale(locale)}
                    >
                      <Text>{t(`lang.${locale}`)}</Text>
                    </Button>
                  ))}
                </View>
              </View>
              {/* Mounted only while the sheet is open, so the status query
               * (`useTelegramLinkStatus`) doesn't fire before the user ever
               * opens Settings — the `Modal` itself stays mounted with
               * `visible={false}` (RN's own behaviour), unlike this child. */}
              {visible && <TelegramLinkSection />}
              <Button variant="destructive" disabled={logout.isPending} onPress={handleLogout}>
                <Text>{t("auth.logout")}</Text>
              </Button>
            </CardContent>
          </Card>
        </Pressable>
      </Pressable>
    </Modal>
  );
}
