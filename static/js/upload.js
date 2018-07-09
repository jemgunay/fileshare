$(document).ready(function() {
    var maxFileSize = parseInt($("#max-file-size").attr("data-size")) / 1024 / 1024;

    // init dropzone
    Dropzone.options.fileInput = {
        paramName: "file-input", // The name that will be used to transfer the file
        maxFilesize: maxFileSize, // MB
        parallelUploads: 3,
        init: function() {
            this.on("success", function(file, response) {
                $("#upload-results-panel").prepend(response);
                $('#upload-results-panel').delay(200).masonry('reloadItems').masonry();

                initUploadForm();
            });
            this.on('error', function(file, errorMessage) {
                errorMessage = errorMessage.trim();
                var refinedError = "";

                if (errorMessage === "already_uploaded") {
                    refinedError = "A copy of '" + file.name + "' has already been uploaded by another user, but not yet published. They must publish it before you can view it."
                }
                else if (errorMessage === "already_published") {
                    refinedError = "A copy of '" + file.name + "' has already been published to memories by another user."
                }
                if (errorMessage === "already_uploaded_self") {
                    refinedError = "You have already uploaded an unpublished copy of '" + file.name + "' - scroll down to see it."
                }
                else if (errorMessage === "already_published_self") {
                    refinedError = "You have already published a copy of '" + file.name + "' to memories."
                }
                else if (errorMessage === "format_not_supported") {
                    refinedError = "The file type of '" + file.name + "' is unsupported."
                }
                else if (errorMessage === "invalid_file") {
                    refinedError = "The file '" + file.name + "' is invalid."
                }
                else if (errorMessage.indexOf("File is too big") !== -1) {
                    refinedError = "The file '" + file.name + "' is too large."
                }
                else {
                    logger.debugLog(errorMessage);
                    refinedError = "A file upload error occurred for '" + file.name + "'."
                }
                var msgEl = $(file.previewElement).find('.dz-error-message');
                msgEl.text(refinedError);

                notifier.queueAlert(refinedError, "warning");

            });
        }
    };

    // allow mixed-height column stacking
    $('#upload-results-panel').masonry({
        itemSelector: '.upload-masonry-item'
    });
    // reload masonry when all images have loaded
    $(window).on("load", function() {
        window.setInterval(function() {
            $('#upload-results-panel').delay(200).masonry('reloadItems').masonry();
        }, 500);
    });

    initUploadForm();
    initUploadTools();
});

function initUploadForm() {
    // set up autocomplete fields
    initUploadTokenfields();

    // set initial states
    setButtonProcessing($(".btn-primary, .btn-danger"), false);

    // for each panel, destroy old events and set up new events
    $(".upload-result-panel").each(function() {
        var panel = $(this);
        var fileName = panel.find("form input[type=hidden]").val();

        // perform publish file request
        panel.find("form .btn-primary").off("click").on("click", function(e) {
            e.preventDefault();
            setButtonProcessing($(this), true);

            // perform request
            performRequest(hostname + "/upload/publish", "POST", $(this).closest("form").serialize(), function (result) {
                result = result.trim();

                setButtonProcessing(panel.find("form .btn-primary"), false);

                if (result === "success") {
                    // show success msg then remove panel
                    notifier.queueAlert("Memory successfully published (" + panel.find("h4").text().trim() + ")!", "success");
                    panel.fadeOut(500, function () {
                        panel.parent(".upload-masonry-item").remove();
                        $('#upload-results-panel').delay(200).masonry('reloadItems').masonry();
                    });

                    //initUploadTokenfields();
                }
                else if (result === "no_description") {
                    panel.find(".btn-primary").attr("title", "Please write a description before publishing.").tooltip('fixTitle').tooltip('show');
                }
                else if (result === "max_description") {
                    panel.find(".btn-primary").attr("title", "Description must be no larger than " + $("#max-description-length").attr("data-size") + " characters.").tooltip('fixTitle').tooltip('show');
                }
                else if (result === "no_tags") {
                    panel.find(".btn-primary").attr("title", "Please specify at least one tag before publishing.").tooltip('fixTitle').tooltip('show');
                }
                else if (result === "max_tags") {
                    panel.find(".btn-primary").attr("title", "Must provide no more than " + $("#max-tags-count").attr("data-size") + " tags.").tooltip('fixTitle').tooltip('show');
                }
                else if (result === "no_people") {
                    panel.find(".btn-primary").attr("title", "Please specify at least one person before publishing.").tooltip('fixTitle').tooltip('show');
                }
                else if (result === "max_people") {
                    panel.find(".btn-primary").attr("title", "Must provide no more than " + $("#max-people-count").attr("data-size") + " people.").tooltip('fixTitle').tooltip('show');
                }
                else if (result === "already_stored") {
                    notifier.queueAlert("A copy of this file has already been stored!", "warning");
                    panel.fadeOut(500, function () {
                        panel.parent(".upload-masonry-item").remove();
                        $('#upload-results-panel').delay(200).masonry('reloadItems').masonry();
                    });
                }
                else {
                    logger.debugLog(result);
                    notifier.queueAlert("A server error occurred (" + fileName + ").", "danger");
                }
            });
        });

        // delete image from user's temp upload area
        panel.find("form .btn-danger").off("click").on("click", function(e) {
            e.preventDefault();
            setButtonProcessing($(this), true);

            // perform request
            performRequest(hostname + "/upload/temp_delete", "POST", $(this).closest("form").serialize(), function (result) {
                result = result.trim();

                setButtonProcessing(panel.find("form .btn-danger"), false);

                if (result === "success") {
                    //notifier.queueAlert("The file has been deleted (" + panel.find("h4").text().trim() + ")!", "success");

                    panel.fadeOut(500, function() {
                        panel.parent(".upload-masonry-item").remove();
                        $('#upload-results-panel').delay(200).masonry('reloadItems').masonry();
                    });
                }
                else if (result === "file_not_found" || result === "file_already_deleted" || result === "delete_error") {
                    notifier.queueAlert("File has already been deleted!", "success");

                    panel.fadeOut(500, function() {
                        panel.parent(".upload-masonry-item").remove();
                        $('#upload-results-panel').delay(200).masonry('reloadItems').masonry();
                    });
                }
                else {
                    logger.debugLog(result);
                    notifier.queueAlert("A server error occurred (" + fileName + ").", "danger");
                }
            });
        });
    });

}

// Populate tokenfields with up to date autocomplete suggestions.
function initUploadTokenfields() {
    performRequest(hostname + "/data?fetch=tags,people", "GET", "", function (result) {
        var tokenfieldSets = [["tags", ".tags-input", false], ["people", ".people-input", false]];
        var parsedData = JSON.parse(result);

        initMetaDataFields(parsedData, tokenfieldSets, null);
    });
}

var individualSelectEnabled = false;

// Set up upload tools UI.
function initUploadTools() {
    // select all
    $("#select-all-btn").on("click", function() {
        $(".upload-result-panel .panel-body").each(function() {
           $(this).addClass("tool-selected");
        });
    });
    // deselect all
    $("#deselect-all-btn").on("click", function() {
        $(".upload-result-panel .panel-body").each(function() {
            $(this).removeClass("tool-selected");
        });
    });
    // select individual
    $("#select-individual-btn").on("click", function() {
        individualSelectEnabled = !individualSelectEnabled;
        if (individualSelectEnabled) {
            $(this).text("Disable Individual Select").addClass("btn-warning");
        } else {
            $(this).text("Enable Individual Select").removeClass("btn-warning");
        }

        $(".upload-result-panel .panel-body").each(function() {
            $(this).on("click", function() {
                if ($(this).hasClass("tool-selected")) {
                    $(this).removeClass("tool-selected");
                } else {
                    $(this).addClass("tool-selected");
                }
            });
        });
    });

    // set description
    $("#set-description-btn").on("click", function() {
        uploadToolDisplayModal("description")
    });
    // set tags
    $("#set-tags-btn").on("click", function() {
        uploadToolDisplayModal("tags")
    });
    // set people
    $("#set-people-btn").on("click", function() {
        uploadToolDisplayModal("people")
    });

    // delete selected
    $("#delete-selected-btn").on("click", function() {
        $(".upload-result-panel .panel-body.tool-selected .btn-danger").each(function() {
            $(this).trigger("click");
        });
    });
}

// Set modal values depending on operation type.
function uploadToolDisplayModal(operation) {
    // ensure some uploads have been selected
    if ($(".tool-selected").length === 0) {
        notifier.queueAlert("Please select at least one upload to edit.", "warning");
        return
    }

    var operationText = {
        "description": ["Set Descriptions...", "Set the description text for all selected uploads to the following...", "Description", function() {
            $(".upload-result-panel .panel-body.tool-selected .description-input").each(function() {
                $(this).val($("#upload-modal .modal-body input").val());
            });
        }],
        "tags": ["Set Tags...", "Set the tags for all selected uploads to the following comma-separated list of tags...", "Tags (comma separated)", function() {
            $(".upload-result-panel .panel-body.tool-selected .tags-input").each(function() {
                $(this).tokenfield("setTokens", $("#upload-modal .modal-body input").val());
            });
        }],
        "people": ["Set People...", "Set the people for all selected uploads to the following comma-separated list of people...", "People (comma separated)", function() {
            $(".upload-result-panel .panel-body.tool-selected .people-input").each(function() {
                $(this).tokenfield("setTokens", $("#upload-modal .modal-body input").val());
            });
        }]
    };

    // set modal UI values
    $("#upload-modal .modal-title").text(operationText[operation][0]);
    $("#upload-modal .modal-body p").text(operationText[operation][1]);
    $("#upload-modal .modal-body input").val("").attr("placeholder", operationText[operation][2]);

    $("#upload-modal").modal("show");
    $("#upload-modal-submit").off("click").on("click", function() {
        operationText[operation][3]();
        $("#upload-modal").modal("hide");
    });
    $("#upload-modal").off("keypress").on("keypress", function(e) {
        if(e.which === 13) {
            operationText[operation][3]();
            $("#upload-modal").modal("hide");
        }
    });
}