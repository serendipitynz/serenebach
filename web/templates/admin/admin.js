// admin.js bootstrap: import the core helpers and feature modules and
// wire them up. Keep vanilla and free of build steps so "drop the binary
// in" still works end-to-end without a bundler. All feature logic lives
// under modules/features/*; this file only imports and initialises it.

import { createI18n } from './modules/core/i18n.js';
import { initToastPromotion } from './modules/core/toast.js';
import { initAppearanceLanguage } from './modules/features/appearance-language.js';
import { initNavigation } from './modules/features/navigation.js';
import { initSortableLists } from './modules/features/sortable-list.js';
import { initLinkKindToggle } from './modules/features/link-form.js';
import { initDateFormatPreview } from './modules/features/date-format-preview.js';
import { initDropToInput } from './modules/features/drop-to-input.js';
import { initUploadForms } from './modules/features/uploads.js';
import { initImageLibrary } from './modules/features/image-library.js';
import { initTemplateAssetPreview } from './modules/features/template-asset-preview.js';
import { initImagePicker } from './modules/features/image-picker.js';
import { initAceEditors, setAIButtonCallback } from './modules/features/ace-editor.js';
import { runAceAI, initAISuggestButtons, initAITestButton } from './modules/features/ai-assist.js';
import { initHintTooltips } from './modules/features/hint-tooltip.js';
import { initCommentDetailModal } from './modules/features/comment-detail-modal.js';
import { initTemplateActions } from './modules/features/template-actions.js';
import { initEntrySaveMode } from './modules/features/entry-save-mode.js';
import { initUnsavedWarn } from './modules/features/unsaved-warn.js';

const sbT = createI18n((typeof window !== 'undefined' && window.__sbI18n) || {});

initToastPromotion();
initAppearanceLanguage();
initNavigation();
initSortableLists(sbT);
initLinkKindToggle();
initDateFormatPreview();
initDropToInput();
initUploadForms();
initImageLibrary();
initTemplateAssetPreview();
initHintTooltips();
initCommentDetailModal();
initTemplateActions();
initEntrySaveMode();

const ace = initAceEditors();
initImagePicker(ace.ready);
setAIButtonCallback(runAceAI);
initAISuggestButtons();
initAITestButton();

initUnsavedWarn(ace.ready);
