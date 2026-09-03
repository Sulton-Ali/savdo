const { getDefaultConfig } = require("expo/metro-config");
const { withNativeWind } = require("nativewind/metro");

// Expo's Metro config has had built-in pnpm/Yarn/Bun/npm monorepo support
// since SDK 52 (docs.expo.dev/guides/monorepos: "Expo configures Metro
// automatically for monorepos. You don't have to manually configure Metro
// when using monorepos if you use expo/metro-config" — manual
// `watchFolders` / `resolver.nodeModulesPaths` are explicitly listed as
// legacy settings to delete, not add). getDefaultConfig(__dirname) already
// resolves `@savdo/api-client` and `@savdo/i18n` from the workspace root.
const config = getDefaultConfig(__dirname);

module.exports = withNativeWind(config, { input: "./src/global.css" });
