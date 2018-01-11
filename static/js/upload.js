$(document).ready(function() {
    // init dropzone
    Dropzone.options.fileInput = {
        paramName: "file-input", // The name that will be used to transfer the file
        maxFilesize: 50, // MB
        init: function() {
            this.on("success", function(file, response) {
                $("#upload-results-panel").append(response);
                $('#upload-results-panel').masonry('reloadItems').masonry();

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
                    refinedError = "You have already uploaded an unpublished copy of '" + file.name + "' - scroll down to find it."
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
                else {
                    console.log(errorMessage);
                    refinedError = "A file upload error occurred for '" + file.name + "'."
                }
                var msgEl = $(file.previewElement).find('.dz-error-message');
                msgEl.text(refinedError);

                notifyAlert(refinedError, "warning");

            });
        }
    };

    // allow mixed-height column stacking
    $('#upload-results-panel').masonry({
        itemSelector: '.upload-masonry-item'
    });
    // reload masonry when all images have loaded
    $(window).on("load", function() {
        $('#upload-results-panel').masonry('reloadItems').masonry();
    });

    initUploadForm();
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
                    notifyAlert("Memory successfully published (" + panel.find("h4").text().trim() + ")!", "success");
                    panel.fadeOut(500, function () {
                        panel.remove();
                        $('#upload-results-panel').masonry('reloadItems').masonry();
                    });

                    initUploadTokenfields();
                }
                else if (result === "no_description") {
                    panel.find(".btn-primary").attr("title", "Please write a description before publishing.").tooltip('show');
                }
                else if (result === "no_tags") {
                    panel.find(".btn-primary").attr("title", "Please specify at least one tag before publishing.").tooltip('show');
                }
                else if (result === "no_people") {
                    panel.find(".btn-primary").attr("title", "Please specify at least one person before publishing.").tooltip('show');
                }
                else if (result === "already_stored") {
                    notifyAlert("A copy of this file has already been stored!", "warning");
                    panel.fadeOut(500, function () {
                        panel.remove();
                        $('#upload-results-panel').masonry('reloadItems').masonry();
                    });
                }
                else {
                    //console.log(result);
                    notifyAlert("A server error occurred (" + fileName + ").", "danger");
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
                    notifyAlert("The file has been deleted " + panel.find("h4").text().trim() + "!", "success");

                    panel.fadeOut(500, function() {
                        panel.remove();
                        $('#upload-results-panel').masonry('reloadItems').masonry();
                    });
                }
                else if (result === "invalid_file" || result === "file_not_found" || result === "file_already_deleted") {
                    console.log(result);
                    notifyAlert("File has already been deleted!", "success")

                    panel.fadeOut(500, function() {
                        panel.remove();
                        $('#upload-results-panel').masonry('reloadItems').masonry();
                    });
                }
                else {
                    console.log(result);
                    notifyAlert("A server error occurred (" + fileName + ").", "danger");
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