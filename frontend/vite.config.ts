import {defineConfig} from 'vitest/config';
import vue from '@vitejs/plugin-vue';
export default defineConfig({plugins:[vue()],base:'./',server:{proxy:{'/api':'http://127.0.0.1:34115'}},test:{environment:'jsdom',exclude:['tests/core.test.ts','node_modules/**']}});
