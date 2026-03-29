const path = require('path');

module.exports = function (api) {
  api.cache(true);

  const expoPackageDir = path.dirname(require.resolve('expo/package.json'));
  const expoBabelPreset = require.resolve('babel-preset-expo', {
    paths: [expoPackageDir],
  });

  return {
    presets: [expoBabelPreset],
  };
};
