// TypeScript has no built-in loader for CSS; NativeWind's Metro/babel plugins
// handle the `import "./global.css"` side-effect import at bundle time, but
// `tsc --noEmit` still needs an ambient module declaration to accept it.
declare module "*.css";
