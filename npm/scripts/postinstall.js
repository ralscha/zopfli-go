'use strict';

const { installBinary } = require('../lib/install');

(async () => {
  try {
    await installBinary();
  } catch (error) {
    console.error(error.message);
    process.exit(1);
  }
})();